// The browser end of the one event stream (issue #5 §9).
//
// The system narrates itself through a single leveled, structured stream. In
// Go that is `log/slog`; here it is this module, backed by the console, with
// the same discipline: a message that reads as a sentence, and everything
// else as named fields. The headless parity runner captures the console, so a
// failing tour step ships its context for free — but only if everything goes
// through here, which is why an ESLint rule forbids bare `console.*` outside
// this file and names this contract when it fires.
//
// Levels are used with intent, as in `docs/logging.md`:
//
//   debug  internal mechanics, off by default — a tile requested, a plan drawn
//   info   one line per meaningful unit of work — a world opened, a lens swapped
//   warn   tolerated, but a human should eventually see it — a tile 404
//   error  an operation failed
//
// The gate is read once, from `?atlas-log=debug` or `localStorage`, because a
// level that can change mid-run makes two captures of one tour disagree.

/** The four levels, in ascending severity. */
export type Level = "debug" | "info" | "warn" | "error";

/** Named facts about the work, never about the code that did it. */
export type Fields = Readonly<Record<string, unknown>>;

const ORDER: Readonly<Record<Level, number>> = { debug: 10, info: 20, warn: 30, error: 40 };

const QUERY_PARAM = "atlas-log";
const STORAGE_KEY = "atlas.log.level";

function isLevel(value: string | null | undefined): value is Level {
  return value === "debug" || value === "info" || value === "warn" || value === "error";
}

/**
 * The level this run speaks at: the query parameter first (a driver can set
 * it without touching storage), then `localStorage`, then `info`.
 *
 * Storage can throw in a hardened context, which is not worth failing a page
 * over: a seam that cannot read a preference still draws a map.
 */
function configuredLevel(): Level {
  try {
    const asked = new URLSearchParams(globalThis.location?.search ?? "").get(QUERY_PARAM);
    if (isLevel(asked)) return asked;
    const stored = globalThis.localStorage?.getItem(STORAGE_KEY);
    if (isLevel(stored)) return stored;
  } catch {
    // A context that refuses the query string or storage gets the default.
  }
  return "info";
}

let threshold = ORDER[configuredLevel()];

/** One record on the stream: a sentence, and the facts around it. */
export interface Logger {
  debug(message: string, fields?: Fields): void;
  info(message: string, fields?: Fields): void;
  warn(message: string, fields?: Fields): void;
  error(message: string, fields?: Fields): void;
  /** A child logger carrying fields every record of it repeats. */
  with(fields: Fields): Logger;
}

const sinks: Readonly<Record<Level, (...args: unknown[]) => void>> = {
  debug: (...args) => console.debug(...args),
  info: (...args) => console.info(...args),
  warn: (...args) => console.warn(...args),
  error: (...args) => console.error(...args),
};

function emit(level: Level, name: string, message: string, fields: Fields): void {
  if (ORDER[level] < threshold) return;
  const payload = { level, logger: name, ...fields };
  sinks[level](`atlas ${name}: ${message}`, payload);
}

/**
 * A logger under a name. The name is the emitting component — a logger
 * concern, deliberately not a vocabulary key (§9), so `op`, `volume`,
 * `world`, `lens` and `path` keep meaning what they mean in the Go stream.
 */
export function logger(name: string, bound: Fields = {}): Logger {
  return {
    debug: (message, fields) => emit("debug", name, message, { ...bound, ...fields }),
    info: (message, fields) => emit("info", name, message, { ...bound, ...fields }),
    warn: (message, fields) => emit("warn", name, message, { ...bound, ...fields }),
    error: (message, fields) => emit("error", name, message, { ...bound, ...fields }),
    with: (fields) => logger(name, { ...bound, ...fields }),
  };
}

/** Open the stream up (or close it down) from a console or a test. */
export function setLevel(level: Level): void {
  threshold = ORDER[level];
}

/** The level the stream is gated at, for the diagnostics object to report. */
export function level(): Level {
  const found = (Object.keys(ORDER) as Level[]).find((name) => ORDER[name] === threshold);
  return found ?? "info";
}
