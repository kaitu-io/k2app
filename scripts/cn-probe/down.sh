#!/usr/bin/env bash
# Release the mainland-China probe box.
#
# Releases (DeleteInstance), never stops. A PostPaid instance that is merely
# stopped keeps billing its system disk, so "stopped" reads as free while it is
# not — that is the failure mode this script exists to prevent.
#
# The VPC / VSwitch / security group are left in place on purpose: they cost
# nothing and make the next up.sh faster. Pass --purge-network to remove them
# too (e.g. when abandoning the probe entirely).

cd "$(dirname "${BASH_SOURCE[0]}")"
. ./lib.sh

PURGE_NETWORK=0
[ "${1:-}" = "--purge-network" ] && PURGE_NETWORK=1

step "Preflight"
preflight

step "Finding probe instances"
IDS=$(ali ecs DescribeInstances --RegionId "$REGION" --InstanceName "$NAME" --PageSize 50 \
  | jget "' '.join(i['InstanceId'] for i in d['Instances']['Instance'])")

if [ -z "$IDS" ]; then
  note "no probe instance found — nothing to release"
else
  for id in $IDS; do
    note "releasing $id"
    ali ecs DeleteInstance --RegionId "$REGION" --InstanceId "$id" --Force true >/dev/null
  done

  step "Confirming release"
  for _ in $(seq 1 30); do
    LEFT=$(ali ecs DescribeInstances --RegionId "$REGION" --InstanceName "$NAME" --PageSize 50 \
      | jget "d['TotalCount']")
    [ "$LEFT" = "0" ] && break
    sleep 5
  done
  [ "$LEFT" = "0" ] || die "instances still present after release: $LEFT"
  note "all probe instances released"
fi

# Deleting an instance normally takes its system disk with it, but a disk whose
# DeleteWithInstance was cleared survives and keeps billing silently. Assert on
# the actual account state rather than trusting the default.
# These are advisory. The release above is what matters, so a check that cannot
# run (missing read permission, API change) must degrade to a visible warning
# rather than abort the script and bury the fact that the instance did go away.
report_orphans() {
  local label="$1" fix="$2" found="$3"
  case "$found" in
    __ERR__) echo "  WARNING: could not check $label (see stderr). Verify by hand." ;;
    "")      note "no $label" ;;
    *)       echo "  WARNING: $label still billing in $REGION: $found"
             echo "           remove with: $fix" ;;
  esac
}

step "Checking for orphaned billable resources"
DISKS=$(ali ecs DescribeDisks --RegionId "$REGION" --Status Available --PageSize 50 \
  | jget "' '.join(x['DiskId'] for x in d['Disks']['Disk'])") || DISKS=__ERR__
report_orphans "unattached disks" \
  "aliyun --profile $PROFILE ecs DeleteDisk --DiskId <id>" "$DISKS"

EIPS=$(ali vpc DescribeEipAddresses --RegionId "$REGION" --Status Available --PageSize 50 \
  | jget "' '.join(x['AllocationId'] for x in d['EipAddresses']['EipAddress'])") || EIPS=__ERR__
report_orphans "unassociated EIPs" \
  "aliyun --profile $PROFILE vpc ReleaseEipAddress --AllocationId <id>" "$EIPS"

SNAPSHOTS=$(ali ecs DescribeSnapshots --RegionId "$REGION" --PageSize 50 \
  | jget "d['TotalCount']") || SNAPSHOTS=__ERR__
note "snapshots/custom images in $REGION: $SNAPSHOTS (these bill for storage)"

if [ "$PURGE_NETWORK" = "1" ]; then
  step "Purging network objects"
  VPC_ID=$(ali vpc DescribeVpcs --RegionId "$REGION" --VpcName "$NAME" \
    | jget "(d['Vpcs']['Vpc'][0]['VpcId'] if d['Vpcs']['Vpc'] else '')")
  if [ -n "$VPC_ID" ]; then
    SG_ID=$(ali ecs DescribeSecurityGroups --RegionId "$REGION" --VpcId "$VPC_ID" \
      --SecurityGroupName "$NAME" \
      | jget "(d['SecurityGroups']['SecurityGroup'][0]['SecurityGroupId'] if d['SecurityGroups']['SecurityGroup'] else '')")
    [ -n "$SG_ID" ] && ali ecs DeleteSecurityGroup --RegionId "$REGION" --SecurityGroupId "$SG_ID" >/dev/null \
      && note "deleted $SG_ID"
    for vsw in $(ali vpc DescribeVSwitches --RegionId "$REGION" --VpcId "$VPC_ID" \
        | jget "' '.join(v['VSwitchId'] for v in d['VSwitches']['VSwitch'])"); do
      ali vpc DeleteVSwitch --RegionId "$REGION" --VSwitchId "$vsw" >/dev/null && note "deleted $vsw"
    done
    ali vpc DeleteVpc --RegionId "$REGION" --VpcId "$VPC_ID" >/dev/null && note "deleted $VPC_ID"
  else
    note "no probe VPC to purge"
  fi
else
  note "network objects kept (free); pass --purge-network to remove them"
fi

echo
echo "done."
