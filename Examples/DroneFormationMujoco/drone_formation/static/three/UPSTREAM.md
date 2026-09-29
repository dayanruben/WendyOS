# three.js provenance

These files are copied unchanged from the Go2 simulator's reviewed vendor
directory (`go/simulator/go2/go2_sim/vendor/`), which pins
[three.js r180 (0.180.0)](https://github.com/mrdoob/three.js/releases/tag/r180)
at commit `0af9729d0c143a86a1d725d6e2c3ad83301f3f34`. The MIT notice is in
`LICENSE` in this directory.

| Upstream path | Vendored file | SHA-256 of vendored file |
| --- | --- | --- |
| `build/three.module.js` | `three.module.js` | `c8211c69345d2e9949dc7a8ac969380497aa0600a5a8ac6a459c8cd02dd9cb8a` |
| `build/three.core.js` | `three.core.js` | `eb077d2417f61d3e6d9264c317cabc4ea35769ed6b0ab533067292a550784c20` |
| `examples/jsm/controls/OrbitControls.js` | `OrbitControls.js` | `06864a0fcb647730bfbc690b6c25a121199d716a3e299a40158509cbd247c3bf` |

The only local change is in `OrbitControls.js`: its import source is
`'./three.module.js'` instead of `'three'`, so the viewer needs no import map,
package install or CDN.
