// The analysis lane (issue #5 §5.4).
//
// An analysis is a **client-side, render-time transformation applied to a
// volume** — the `.atlas` file is blind to it and is never mutated by it. The
// volume is one input; an analysis may declare others (user markup, a live
// feed, a hook into a running game), and how those are supplied is the
// consumer's adapter concern, never this lane's.
//
// Cell systems are the founding family and set the conformance bar. Later
// families export from here beside them.

export * from "./cellsystems/index.ts";
