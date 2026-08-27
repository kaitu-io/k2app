#!/usr/bin/env bash
# Bring up the mainland-China Windows probe box. Idempotent: re-running while a
# probe is already alive prints its connection details instead of creating a
# second one.
#
# Network objects (VPC / VSwitch / security group) are FREE on Aliyun, so they
# are created once and left behind; only the instance costs money. down.sh
# therefore deletes the instance and keeps the network.

cd "$(dirname "${BASH_SOURCE[0]}")"
. ./lib.sh

step "Preflight"
preflight

step "Checking for an existing probe"
EXISTING=$(ali ecs DescribeInstances --RegionId "$REGION" \
  --InstanceName "$NAME" --PageSize 10 \
  | jget "json.dumps([{'id':i['InstanceId'],'status':i['Status'],'ip':(i.get('PublicIpAddress',{}).get('IpAddress') or [''])[0]} for i in d['Instances']['Instance']])")

if [ "$EXISTING" != "[]" ]; then
  note "a probe already exists:"
  printf '%s\n' "$EXISTING" | python3 -m json.tool
  note "connection password is in $PASSWORD_FILE"
  note "run scripts/cn-probe/down.sh to release it"
  exit 0
fi

step "Finding a zone with PostPaid stock for $INSTANCE_TYPE"
ZONE=$(ali ecs DescribeAvailableResource --RegionId "$REGION" \
  --DestinationResource InstanceType --InstanceChargeType PostPaid \
  --InstanceType "$INSTANCE_TYPE" \
  | jget "next(z['ZoneId'] for z in d['AvailableZones']['AvailableZone'] if z.get('StatusCategory')=='WithStock')") \
  || die "no zone in $REGION currently has PostPaid stock for $INSTANCE_TYPE"
note "zone: $ZONE"

step "Ensuring VPC"
VPC_ID=$(ali vpc DescribeVpcs --RegionId "$REGION" --VpcName "$NAME" \
  | jget "(d['Vpcs']['Vpc'][0]['VpcId'] if d['Vpcs']['Vpc'] else '')")
if [ -z "$VPC_ID" ]; then
  VPC_ID=$(ali vpc CreateVpc --RegionId "$REGION" --VpcName "$NAME" \
    --CidrBlock 172.16.0.0/16 | jget "d['VpcId']")
  note "created $VPC_ID"
  # CreateVpc returns before the VPC is usable; VSwitch creation fails if we race it.
  for _ in $(seq 1 30); do
    st=$(ali vpc DescribeVpcAttribute --RegionId "$REGION" --VpcId "$VPC_ID" | jget "d['Status']")
    [ "$st" = "Available" ] && break
    sleep 2
  done
  [ "$st" = "Available" ] || die "VPC $VPC_ID never became Available"
else
  note "reusing $VPC_ID"
fi

step "Ensuring VSwitch in $ZONE"
VSW_ID=$(ali vpc DescribeVSwitches --RegionId "$REGION" --VpcId "$VPC_ID" --ZoneId "$ZONE" \
  | jget "(d['VSwitches']['VSwitch'][0]['VSwitchId'] if d['VSwitches']['VSwitch'] else '')")
if [ -z "$VSW_ID" ]; then
  VSW_ID=$(ali vpc CreateVSwitch --RegionId "$REGION" --VpcId "$VPC_ID" --ZoneId "$ZONE" \
    --VSwitchName "$NAME" --CidrBlock 172.16.1.0/24 | jget "d['VSwitchId']")
  note "created $VSW_ID"
else
  note "reusing $VSW_ID"
fi

step "Ensuring security group"
SG_ID=$(ali ecs DescribeSecurityGroups --RegionId "$REGION" --VpcId "$VPC_ID" \
  --SecurityGroupName "$NAME" \
  | jget "(d['SecurityGroups']['SecurityGroup'][0]['SecurityGroupId'] if d['SecurityGroups']['SecurityGroup'] else '')")
if [ -z "$SG_ID" ]; then
  SG_ID=$(ali ecs CreateSecurityGroup --RegionId "$REGION" --VpcId "$VPC_ID" \
    --SecurityGroupName "$NAME" | jget "d['SecurityGroupId']")
  note "created $SG_ID"
else
  note "reusing $SG_ID"
fi

# Scope RDP to this machine only, and re-apply every run because a home/office
# IP changes. Old rules are revoked first so the allow-list never accumulates
# stale addresses that would silently widen access over time.
MY_IP="$(my_public_ip)"
step "Scoping RDP (3389) to $MY_IP/32"
ali ecs DescribeSecurityGroupAttribute --RegionId "$REGION" --SecurityGroupId "$SG_ID" \
  --Direction ingress \
  | jget "'\n'.join(p['SourceCidrIp'] for p in d['Permissions']['Permission'] if p.get('PortRange')=='3389/3389')" \
  | while read -r cidr; do
      [ -n "$cidr" ] || continue
      [ "$cidr" = "$MY_IP/32" ] && continue
      note "revoking stale rule for $cidr"
      ali ecs RevokeSecurityGroup --RegionId "$REGION" --SecurityGroupId "$SG_ID" \
        --IpProtocol tcp --PortRange 3389/3389 --SourceCidrIp "$cidr" >/dev/null
    done
ali ecs AuthorizeSecurityGroup --RegionId "$REGION" --SecurityGroupId "$SG_ID" \
  --IpProtocol tcp --PortRange 3389/3389 --SourceCidrIp "$MY_IP/32" \
  --Description "RDP from operator" >/dev/null
note "3389 open to $MY_IP/32 only"

step "Generating Windows password"
# Aliyun requires 8-30 chars containing 3 of {upper, lower, digit, special}.
PASSWORD=$(python3 - <<'PY'
import secrets, string
pools = [string.ascii_uppercase, string.ascii_lowercase, string.digits, "!@#%^_-+="]
chars = [secrets.choice(p) for p in pools for _ in range(4)]
secrets.SystemRandom().shuffle(chars)
print("".join(chars))
PY
)
mkdir -p "$(dirname "$PASSWORD_FILE")"
( umask 077; printf '%s\n' "$PASSWORD" > "$PASSWORD_FILE" )
note "saved to $PASSWORD_FILE (mode 600)"

# Validate the whole request server-side before spending anything. Cheap
# read-only calls are NOT sufficient evidence that a launch will succeed:
# DescribePrice returns a quote and DescribeAvailableResource reports
# WithStock even for an account that is forbidden from buying in this region.
# The first run of this script died here on RealNameAuthenticationError with
# every earlier check green — mainland regions require real-name
# authentication, and only RunInstances says so.
step "Validating the launch request (DryRun — costs nothing)"
if ! DRY=$(ali ecs RunInstances --RegionId "$REGION" --ZoneId "$ZONE" \
  --ImageId "$IMAGE_ID" --InstanceType "$INSTANCE_TYPE" \
  --SystemDisk.Category "$DISK_CATEGORY" --SystemDisk.Size "$DISK_SIZE" \
  --VSwitchId "$VSW_ID" --SecurityGroupId "$SG_ID" \
  --InstanceChargeType PostPaid \
  --InternetChargeType PayByTraffic --InternetMaxBandwidthOut "$BANDWIDTH_OUT" \
  --Password "$PASSWORD" --Amount 1 --DryRun true 2>&1); then
  case "$DRY" in
    *RealNameAuthenticationError*)
      die "this account has not completed real-name authentication, so it cannot
       create instances in mainland China regions. Every mainland provider
       requires this by law, so switching to Tencent/Huawei will not help.
       Resolve by authenticating this account, or point K2_CN_PROFILE at an
       already-authenticated one." ;;
    *DryRunOperation*|*Success*) : ;;
    *)
      die "the launch request was rejected before any resource was created:
$DRY" ;;
  esac
fi
note "request accepted"

step "Launching instance"
INSTANCE_ID=$(ali ecs RunInstances --RegionId "$REGION" --ZoneId "$ZONE" \
  --ImageId "$IMAGE_ID" --InstanceType "$INSTANCE_TYPE" \
  --SystemDisk.Category "$DISK_CATEGORY" --SystemDisk.Size "$DISK_SIZE" \
  --VSwitchId "$VSW_ID" --SecurityGroupId "$SG_ID" \
  --InstanceChargeType PostPaid \
  --InternetChargeType PayByTraffic --InternetMaxBandwidthOut "$BANDWIDTH_OUT" \
  --InstanceName "$NAME" --HostName "k2-cn-probe" \
  --Password "$PASSWORD" \
  --Tag.1.Key "$TAG_KEY" --Tag.1.Value "$TAG_VALUE" \
  --Amount 1 | jget "d['InstanceIdSets']['InstanceIdSet'][0]")
note "instance: $INSTANCE_ID"

step "Waiting for Running (Windows first boot takes a few minutes)"
for _ in $(seq 1 60); do
  STATUS=$(ali ecs DescribeInstances --RegionId "$REGION" --InstanceIds "[\"$INSTANCE_ID\"]" \
    | jget "d['Instances']['Instance'][0]['Status']")
  [ "$STATUS" = "Running" ] && break
  sleep 10
done
[ "$STATUS" = "Running" ] || die "instance stuck in status '$STATUS'"

PUBLIC_IP=$(ali ecs DescribeInstances --RegionId "$REGION" --InstanceIds "[\"$INSTANCE_ID\"]" \
  | jget "d['Instances']['Instance'][0]['PublicIpAddress']['IpAddress'][0]")

cat <<EOF

================ probe is up ================
  instance : $INSTANCE_ID  ($INSTANCE_TYPE, $REGION/$ZONE)
  public IP: $PUBLIC_IP
  RDP      : $PUBLIC_IP:3389   user Administrator
  password : $PASSWORD_FILE
  cost     : ~\$0.048/hr + PayByTraffic egress

  Windows needs a few more minutes after "Running" before RDP and Cloud
  Assistant answer. Drive it without RDP via:
    aliyun --profile $PROFILE ecs RunCommand --RegionId $REGION \\
      --InstanceId.1 $INSTANCE_ID --Type RunPowerShellScript \\
      --CommandContent "\$(base64 < some.ps1)" --ContentEncoding Base64

  RELEASE IT WHEN DONE: scripts/cn-probe/down.sh
  A PostPaid instance keeps billing its system disk even when stopped, so
  stopping is not the same as releasing.
=============================================
EOF
