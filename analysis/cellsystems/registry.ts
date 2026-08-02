// The registry: which systems exist, in which order, and how a place crosses
// between them.
//
// A registry is an immutable list. That is deliberate — a mutable module-level
// registry is the same shared client state issue #5 §5.4 just removed from the
// systems themselves, and it would make "which systems exist" depend on import
// order. `cellSystems` is the default list; `.with(system)` answers a new
// registry, so a consumer that wants a third system composes one and passes it
// where it is needed.

import type { CellSystem } from "./contract.ts";
import type { Ground } from "./ground.ts";
import { extentArea } from "./ground.ts";
import { geohashSystem } from "./geohash.ts";
import { s2System } from "./s2.ts";

/** An immutable, ordered set of cell systems. Order is the navigator's order. */
export class CellSystemRegistry {
  readonly systems: readonly CellSystem[];

  constructor(systems: readonly CellSystem[]) {
    const seen = new Set<string>();
    for (const system of systems) {
      if (seen.has(system.slug)) {
        throw new Error(`cellsystems: two systems both call themselves "${system.slug}"`);
      }
      seen.add(system.slug);
    }
    this.systems = Object.freeze([...systems]);
  }

  /** A new registry with these systems appended, in the order given. */
  with(...more: readonly CellSystem[]): CellSystemRegistry {
    return new CellSystemRegistry([...this.systems, ...more]);
  }

  /** The system with this slug, or undefined. */
  get(slug: string): CellSystem | undefined {
    return this.systems.find((system) => system.slug === slug);
  }

  /** The system with this slug, or a named failure. */
  require(slug: string): CellSystem {
    const system = this.get(slug);
    if (!system) {
      const known = this.systems.map((candidate) => candidate.slug).join(", ");
      throw new Error(`cellsystems: no system named "${slug}" (registered: ${known || "none"})`);
    }
    return system;
  }

  /** Those willing to divide this ground, in registry order. */
  applicableTo(ground: Ground): readonly CellSystem[] {
    return this.systems.filter((system) => system.appliesTo(ground));
  }
}

/**
 * The systems Atlas ships, in the order the navigator offers them. Geohash
 * first because it divides anything; S2 second because it asks the ground a
 * question first.
 */
export const cellSystems = new CellSystemRegistry([geohashSystem, s2System]);

/** Those of a registry willing to divide this ground, in registry order. */
export function applicableSystems(
  ground: Ground,
  registry: CellSystemRegistry = cellSystems,
): readonly CellSystem[] {
  return registry.applicableTo(ground);
}

/**
 * Carry a place across systems: the cell in the new hierarchy holding the old
 * cell's centre, taken to the depth whose cells cover closest to the same
 * ground.
 *
 * The two hierarchies share no boundaries, so what survives the translation is
 * the point and the precision. Area is compared on a log scale, because levels
 * grow geometrically and "closest" should mean closest in kind, not in pixels.
 * The root carries to the root, which is the only place the two hierarchies
 * agree exactly.
 */
export function equivalentCell(
  ground: Ground,
  from: CellSystem,
  to: CellSystem,
  id: string,
): string {
  if (!id) return "";
  const source = from.on(ground);
  const target = to.on(ground);
  const centre = source.center(id);
  const area = extentArea(source.bbox(id));
  const floor = to.maxLevel(ground);
  let held = "";
  let best = "";
  let bestFit = Infinity;
  while (target.level(held) < floor) {
    const next = target.descendTarget(held, centre);
    if (!next) break;
    const nextArea = extentArea(target.bbox(next));
    const fit = Math.abs(Math.log(nextArea / area));
    if (fit < bestFit) {
      best = next;
      bestFit = fit;
    }
    // Areas only shrink on the way down: past the target, deeper is worse.
    if (nextArea <= area) break;
    held = next;
  }
  return best;
}
