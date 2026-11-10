import { describe, expect, it } from "vitest";
import type { CloudInstance, NodeDistributionItem } from "@/lib/api";
import { fleetCountries, fleetProviders, mergeFleetRows, resetCycle } from "@/lib/node-fleet";

const node = (name: string, ipv4: string, country: string, activeDevices: number) =>
  ({ name, ipv4, country, activeDevices }) as NodeDistributionItem;
let nextId = 1;
const instance = (ip_address: string) => ({ id: nextId++, ip_address }) as CloudInstance;

describe("mergeFleetRows", () => {
  const nodes = [
    node("sg-b", "2.2.2.2", "SG", 3),
    node("jp-a", "1.1.1.1", "JP", 1),
    node("sg-a", "3.3.3.3", "SG", 71),
  ];

  it("joins by IPv4, keeps nodes with no instance, sorts busiest first per country", () => {
    const rows = mergeFleetRows(nodes, [instance("3.3.3.3")], { includeUnregistered: true });
    expect(rows.map((r) => r.ipv4)).toEqual(["1.1.1.1", "3.3.3.3", "2.2.2.2"]);
    expect(rows[1].instance?.ip_address).toBe("3.3.3.3");
    expect(rows[2].instance).toBeUndefined();
    expect(fleetCountries(rows)).toEqual(["JP", "SG"]);
  });

  it("lists instances without a node last, and only when asked", () => {
    const fresh = [instance("9.9.9.9")];
    expect(mergeFleetRows(nodes, fresh, { includeUnregistered: true }).at(-1)?.ipv4).toBe("9.9.9.9");
    expect(mergeFleetRows(nodes, fresh, { includeUnregistered: false })).toHaveLength(3);
  });

  it("never loses an instance to an empty or repeated IP", () => {
    const pending = [instance(""), instance(""), instance("1.1.1.1"), instance("1.1.1.1")];
    const rows = mergeFleetRows(nodes, pending, { includeUnregistered: true });
    expect(rows).toHaveLength(6);
    expect(rows.filter((r) => r.instance)).toHaveLength(4);
    expect(new Set(rows.map((r) => r.key)).size).toBe(6);
  });
});

describe("fleetProviders", () => {
  const withProvider = (ip: string, provider: string) =>
    ({ ...instance(ip), provider }) as CloudInstance;

  it("counts machines per provider, most first, ignoring rows with no instance", () => {
    const nodes = [node("a", "1.1.1.1", "US", 0), node("b", "2.2.2.2", "US", 0), node("c", "3.3.3.3", "JP", 0)];
    const rows = mergeFleetRows(
      nodes,
      [withProvider("1.1.1.1", "vultr"), withProvider("2.2.2.2", "bandwagon"), withProvider("9.9.9.9", "bandwagon")],
      { includeUnregistered: true },
    );
    expect(fleetProviders(rows)).toEqual([
      { provider: "bandwagon", count: 2 },
      { provider: "vultr", count: 1 },
    ]);
  });
});

describe("resetCycle", () => {
  it("reads the day and clock time in UTC, not the browser zone", () => {
    expect(resetCycle(Date.UTC(2026, 10, 23, 5, 36) / 1000)).toEqual({ day: 23, label: "每月 23 日 05:36 UTC" });
    // 20:00 UTC on the 23rd is already the 24th in +07/+08, and 00:00 on the 1st is still the 31st in the Americas.
    expect(resetCycle(Date.UTC(2026, 10, 23, 20, 0) / 1000)?.day).toBe(23);
    expect(resetCycle(Date.UTC(2026, 11, 1, 0, 0) / 1000)?.day).toBe(1);
  });

  it("returns null when the provider gave no reset time", () => {
    expect(resetCycle(0)).toBeNull();
    expect(resetCycle(-1)).toBeNull();
  });
});
