#!/usr/bin/env node
// Fails (non-zero exit) if src/locales/pt-BR.json and en.json don't have the
// exact same set of keys - a key added to one locale during the page-by-page
// i18n migration (see .specs/... plan) and forgotten in the other would
// otherwise only surface as a silent fallback/missing string in production,
// never caught by tsc (no typed-keys mechanism exists here) or by any
// existing lint (no ESLint in this repo).
import { readFileSync } from "node:fs";

const LOCALES_DIR = new URL("../src/locales/", import.meta.url);

const LOCALES = ["pt-BR", "en"];

function loadLocale(name) {
  return JSON.parse(readFileSync(new URL(`${name}.json`, LOCALES_DIR), "utf8"));
}

// Depth-first walk collecting every leaf key path as a dot-joined string
// (e.g. "settingsPage.companyProfile.name") - structure, not values, is
// what must match between locales.
function collectKeyPaths(obj, prefix = "") {
  const paths = [];
  for (const [key, value] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (value !== null && typeof value === "object" && !Array.isArray(value)) {
      paths.push(...collectKeyPaths(value, path));
    } else {
      paths.push(path);
    }
  }
  return paths;
}

function diff(aName, aPaths, bName, bPaths) {
  const bSet = new Set(bPaths);
  return aPaths.filter((p) => !bSet.has(p)).map((p) => `  ${p} (in ${aName}, missing from ${bName})`);
}

const [baseName, ...otherNames] = LOCALES;
const baseline = collectKeyPaths(loadLocale(baseName));

let hasMismatch = false;
for (const name of otherNames) {
  const paths = collectKeyPaths(loadLocale(name));
  const missingFromOther = diff(baseName, baseline, name, paths);
  const missingFromBase = diff(name, paths, baseName, baseline);
  const mismatches = [...missingFromOther, ...missingFromBase];
  if (mismatches.length > 0) {
    hasMismatch = true;
    console.error(`i18n key parity mismatch between ${baseName}.json and ${name}.json:`);
    for (const line of mismatches.sort()) console.error(line);
  }
}

if (hasMismatch) {
  process.exit(1);
}

console.log(`i18n key parity OK (${LOCALES.join(", ")}): ${baseline.length} keys each.`);
