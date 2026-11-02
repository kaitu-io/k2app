import type { CloudInstance, NodeDistributionItem } from "@/lib/api";

// One machine on the node page. A row has a node, a cloud instance, or both:
// self-hosted nodes have no instance, a fresh instance has no node yet.
export interface FleetRow {
  key: string; // an IP can be empty or shared, so it cannot identify a row
  ipv4: string;
  node?: NodeDistributionItem;
  instance?: CloudInstance;
}

export function rowCountry(row: FleetRow): string {
  return row.node?.country ?? "";
}

export function fleetCountries(rows: FleetRow[]): string[] {
  return [...new Set(rows.map(rowCountry).filter(Boolean))].sort();
}

// Joins on IPv4, the one key both sides share. Rows sort by country, then
// busiest first, so a skewed country reads top to bottom; instances without a
// node go last.
export function mergeFleetRows(
  nodes: NodeDistributionItem[],
  instances: CloudInstance[],
  { includeUnregistered }: { includeUnregistered: boolean },
): FleetRow[] {
  const claimed = new Set<number>();
  const rows: FleetRow[] = nodes.map((node) => {
    const instance = instances.find((i) => i.ip_address === node.ipv4 && !claimed.has(i.id));
    if (instance) claimed.add(instance.id);
    return { key: `node-${node.ipv4}`, ipv4: node.ipv4, node, instance };
  });
  rows.sort(
    (a, b) =>
      rowCountry(a).localeCompare(rowCountry(b)) ||
      (b.node?.activeDevices ?? 0) - (a.node?.activeDevices ?? 0) ||
      (a.node?.name ?? "").localeCompare(b.node?.name ?? ""),
  );
  // Every instance stays reachable: this page is where it gets changed or deleted.
  if (includeUnregistered) {
    for (const instance of instances) {
      if (!claimed.has(instance.id)) {
        rows.push({ key: `instance-${instance.id}`, ipv4: instance.ip_address, instance });
      }
    }
  }
  return rows;
}
