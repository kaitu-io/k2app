import type { CloudInstance, NodeDistributionItem } from "@/lib/api";

// One machine on the node page. A row has a node, a cloud instance, or both:
// self-hosted nodes have no instance, a fresh instance has no node yet.
export interface FleetRow {
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
  const byIp = new Map(instances.map((instance) => [instance.ip_address, instance]));
  const rows: FleetRow[] = nodes.map((node) => {
    const instance = byIp.get(node.ipv4);
    byIp.delete(node.ipv4);
    return { ipv4: node.ipv4, node, instance };
  });
  rows.sort(
    (a, b) =>
      rowCountry(a).localeCompare(rowCountry(b)) ||
      (b.node?.activeDevices ?? 0) - (a.node?.activeDevices ?? 0) ||
      (a.node?.name ?? "").localeCompare(b.node?.name ?? ""),
  );
  if (includeUnregistered) {
    for (const [ipv4, instance] of byIp) rows.push({ ipv4, instance });
  }
  return rows;
}
