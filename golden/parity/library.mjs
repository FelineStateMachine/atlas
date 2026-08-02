// The fixture library, assembled the same way for every consumer of it.
//
// The installed registry holds many builds of many volumes and the fold that
// decides which one serves picks whatever happened to be newest that day. A
// baseline is only reproducible if the library it was taken from is, so both
// the capture (`capture.mjs`, against the golden reference) and the gate
// (`compare.mjs`, against the candidate) point their application at a link
// farm holding exactly the files `FIXTURES.json` names -- which makes the
// fold a no-op and the library the same everywhere.
//
// One volume is not in the registry and never will be: the city was built for
// the fixture set rather than installed. Its entry carries `builtInto`, the
// repository-relative directory it was built into, and ATLAS_GOLDEN_CITY_DIR
// moves that.
//
// The registry is read-only here: this reads and links, never writes.

import { createHash } from "node:crypto";
import { createReadStream, mkdirSync, readFileSync, rmSync, statSync, symlinkSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "../..");

/** The fixture set, as committed. */
export function fixtures() {
  return JSON.parse(readFileSync(join(here, "FIXTURES.json"), "utf8"));
}

/** Where one fixture's bundle file is read from. Read-only, both cases. */
export function sourceOf(volume) {
  const registry = process.env.ATLAS_GOLDEN_BUNDLES ||
    join(homedir(), "Library/Application Support/dev.felinestatemachine.atlas/bundles");
  return volume.builtInto
    ? resolve(repoRoot, process.env.ATLAS_GOLDEN_CITY_DIR || volume.builtInto, volume.file)
    : join(registry, volume.file);
}

const sha256 = (path) => new Promise((done, fail) => {
  const hash = createHash("sha256");
  createReadStream(path).on("error", fail).on("data", (chunk) => hash.update(chunk))
    .on("end", () => done(hash.digest("hex")));
});

/**
 * Build the link farm and answer where it is.
 *
 * It is rebuilt from scratch every run: a link left over from an earlier
 * fixture set would put one more volume on the select and move every
 * baseline's volume list. Sizes are checked always and digests only when
 * asked, because the set is half a gigabyte and a byte count catches the
 * mistake that actually happens -- the wrong build under the right name.
 */
export async function linkFarm(volumes, { verify = false, into } = {}) {
  const farm = into ?? join(here, ".bundles");
  rmSync(farm, { recursive: true, force: true });
  mkdirSync(farm, { recursive: true });
  for (const volume of volumes) {
    const source = sourceOf(volume);
    let info;
    try {
      info = statSync(source);
    } catch {
      const lines = [`${volume.slug}: no bundle at ${source}`];
      if (volume.builtInto) {
        lines.push("this one is built rather than installed: golden/fixtures/README.md,");
        lines.push("'The city fixture', or point ATLAS_GOLDEN_CITY_DIR at where it was built");
      }
      throw new Error(lines.join("\n"));
    }
    if (info.size !== volume.bytes) {
      throw new Error(`${volume.file}: ${info.size} bytes, the fixture names ${volume.bytes}`);
    }
    if (verify) {
      const digest = await sha256(source);
      if (digest !== volume.sha256) {
        throw new Error(`${volume.file}: sha256 ${digest}, the fixture names ${volume.sha256}`);
      }
    }
    symlinkSync(source, join(farm, volume.file));
  }
  return farm;
}
