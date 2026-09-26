/**
 * Mermaid conformance suite for the tm graph writer (spec §16.6).
 *
 * For each corpus file, under both mermaid11 (11.17.2) and mermaid12 (12.0.0):
 *   1. Calls mermaid.parse — any throw fails the suite.
 *   2. Calls mermaid.mermaidAPI.getDiagramFromText, reads db.getSubGraphs(),
 *      and fails if any node's subgraph differs from the sidecar.
 *
 * Run:  node parse.mjs
 * Exit: 0 on full pass, 1 on any failure.
 */

import { readFileSync, readdirSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

// ── DOM shim ─────────────────────────────────────────────────────────────────
// Must be set up BEFORE importing Mermaid (Mermaid reads these at module load).
import { JSDOM } from 'jsdom';

const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', {
  pretendToBeVisual: true,
});
const { window } = dom;

// Use defineProperty for globals that Node.js marks read-only (e.g. navigator).
function setGlobal(name, value) {
  try {
    globalThis[name] = value;
  } catch {
    try {
      Object.defineProperty(globalThis, name, {
        value,
        writable: true,
        configurable: true,
      });
    } catch {
      // Already set or cannot be overridden — skip.
    }
  }
}

setGlobal('window', window);
setGlobal('document', window.document);
setGlobal('navigator', window.navigator);
setGlobal('Element', window.Element);
setGlobal('HTMLElement', window.HTMLElement);
// SVGElement is not available in jsdom; fall back to Element.
setGlobal('SVGElement', window.SVGElement ?? window.Element);
setGlobal('Node', window.Node);
setGlobal('DOMParser', window.DOMParser);
setGlobal('MutationObserver', window.MutationObserver);
// Some Mermaid internals access requestAnimationFrame / cancelAnimationFrame.
if (typeof globalThis.requestAnimationFrame === 'undefined') {
  setGlobal('requestAnimationFrame', cb => setTimeout(cb, 0));
  setGlobal('cancelAnimationFrame', id => clearTimeout(id));
}

// ── Import Mermaid versions (after globals) ───────────────────────────────────
const [{ default: mermaid11 }, { default: mermaid12 }] = await Promise.all([
  import('mermaid11'),
  import('mermaid12'),
]);

mermaid11.initialize({ startOnLoad: false, securityLevel: 'loose' });
mermaid12.initialize({ startOnLoad: false, securityLevel: 'loose' });

// ── Corpus ────────────────────────────────────────────────────────────────────
const __dirname = dirname(fileURLToPath(import.meta.url));
const corpusDir = join(__dirname, 'corpus');

let mmdFiles;
try {
  mmdFiles = readdirSync(corpusDir)
    .filter(f => f.endsWith('.mmd'))
    .sort();
} catch {
  console.error(
    'conformance: corpus directory not found — ' +
    'run: go run ./internal/tools/corpus -out conformance/corpus',
  );
  process.exit(1);
}

if (mmdFiles.length === 0) {
  console.error('conformance: no .mmd files found in corpus/');
  process.exit(1);
}

// ── Run suite ─────────────────────────────────────────────────────────────────
const failures = [];
let totalChecks = 0;

/**
 * Build a nodeId → subgraphId map from getSubGraphs() output.
 * @param {Array} subgraphs
 * @returns {Object}
 */
function buildNodeToSg(subgraphs) {
  const map = {};
  for (const sg of subgraphs) {
    if (!Array.isArray(sg.nodes)) continue;
    for (const nodeId of sg.nodes) {
      map[nodeId] = sg.id;
    }
  }
  return map;
}

/**
 * Run both checks for one (mermaid version, corpus file) pair.
 */
async function checkOne(label, mermaid, mmdFile) {
  const text = readFileSync(join(corpusDir, mmdFile), 'utf8');
  const sidecarPath = join(corpusDir, mmdFile.slice(0, -4) + '.json');
  const sidecar = JSON.parse(readFileSync(sidecarPath, 'utf8'));

  // Check 1: mermaid.parse must not throw.
  try {
    await mermaid.parse(text);
  } catch (e) {
    failures.push(`[${label}] ${mmdFile}: mermaid.parse threw: ${e.message}`);
    return false;
  }

  // Check 2: subgraph membership must match the sidecar.
  let nodeToSg;
  try {
    const diag = await mermaid.mermaidAPI.getDiagramFromText(text);
    const subgraphs = diag.db.getSubGraphs();
    nodeToSg = buildNodeToSg(subgraphs);
  } catch (e) {
    failures.push(
      `[${label}] ${mmdFile}: getDiagramFromText threw: ${e.message}`,
    );
    return false;
  }

  let ok = true;
  for (const [nodeId, expected] of Object.entries(sidecar)) {
    const actual = nodeToSg[nodeId];
    if (actual !== expected) {
      ok = false;
      failures.push(
        `[${label}] ${mmdFile}: node "${nodeId}" — ` +
        `expected subgraph "${expected}", got "${actual ?? '<not assigned>'}"`,
      );
    }
  }
  return ok;
}

for (const mmdFile of mmdFiles) {
  for (const [label, mermaid] of [
    ['mermaid11', mermaid11],
    ['mermaid12', mermaid12],
  ]) {
    totalChecks++;
    await checkOne(label, mermaid, mmdFile);
  }
}

// ── Report ─────────────────────────────────────────────────────────────────────
const passed = totalChecks - failures.length;
console.log(
  `\nconformance: ${passed}/${totalChecks} passed` +
  ` (${mmdFiles.length} files × 2 mermaid versions)\n`,
);

if (failures.length > 0) {
  for (const f of failures) {
    console.error(`FAIL: ${f}`);
  }
  process.exit(1);
}
