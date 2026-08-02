// The inert half of the application, for the modules the cell math sits
// beside but never calls.
//
// `frontend/src/grid.js` is two things in one file: the plan and its style
// tokens, which are pure, and the controller around them, which talks to the
// DOM, the chart's camera and the pin registry. Importing that file under
// node executes its import list, and the controller half drags in
// OpenLayers' map, globe.gl and a document. This module stands in for
// exactly those four imports — `dom`, `detail`, `navigation`, `features` —
// so the pure half loads with nothing emulated and nothing patched.
//
// Every export here throws if it is ever called: the gate must fail loudly
// rather than record a plan that only exists because a stub said nothing.
// `elements` is the one exception — grid.js reads element handles at module
// scope in a few places the gate does not reach, and a proxy that answers
// with inert objects is closer to a headless browser than a throw would be.

function refuse(name) {
  return () => {
    throw new Error(
      `golden/analysis: the current-tree engine called ${name}(), which is application ` +
      `behaviour, not cell math. The vector gate drives the pure half only ` +
      `(issue #5 §5.4); if a plan now depends on the controller, the contract moved.`,
    );
  };
}

const inertElement = new Proxy({}, {
  get(_, key) {
    if (key === "hidden" || key === "value" || key === "textContent" || key === "title") return "";
    return () => {};
  },
  set() {
    return true;
  },
});

export const elements = new Proxy({}, { get: () => inertElement });

export const closeDetail = refuse("closeDetail");
export const releaseCameraReturn = refuse("releaseCameraReturn");
export const viewMaxZoom = refuse("viewMaxZoom");
export const refreshPinRendering = refuse("refreshPinRendering");
