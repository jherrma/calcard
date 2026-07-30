#!/usr/bin/env node
/**
 * Generates public/open-source.json — the npm half of the open-source
 * attribution list (story 101).
 *
 * Deliberately dependency-free: it reads the metadata pnpm already installed
 * (node_modules/**\/package.json) instead of pulling in a license-checker
 * package, which would enlarge the very dependency set we are attributing. No
 * network access, no lockfile parsing, no new devDependency.
 *
 * Scope: the transitive closure of the RUNTIME `dependencies` in
 * webinterface/package.json — i.e. what can end up in the shipped SPA.
 * devDependencies (vitest, eslint, prettier, …) are build tooling that is never
 * distributed, so attributing them would be noise. `optionalDependencies` are
 * skipped too: they are platform binaries (esbuild/@parcel/watcher builds) that
 * likewise never reach the browser.
 *
 * The license is taken from each package's DECLARED `license` field — for npm
 * that is the authoritative statement, unlike the Go side where we have to
 * sniff LICENSE files. A package with no usable declaration is reported as
 * "unknown" rather than guessed at.
 *
 * Usage (from webinterface/): pnpm run gen:licenses
 * Re-run it after any dependency change and commit the result. Output is
 * deterministic (sorted, no timestamp) so an unchanged dependency set produces
 * an empty diff.
 */
import { existsSync, readFileSync, writeFileSync, mkdirSync, realpathSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(scriptDir, '..');
const outFile = path.join(root, 'public', 'open-source.json');

const UNKNOWN = 'unknown';

function readJson(file) {
  try {
    return JSON.parse(readFileSync(file, 'utf8'));
  } catch {
    return null;
  }
}

/**
 * Node-style resolution: walk up from `fromDir` looking for
 * `<dir>/node_modules/<name>/package.json`. Never escapes the project root.
 *
 * The result is realpath'd, which is what makes pnpm's isolated layout
 * traversable: the root `node_modules/<name>` entries are symlinks into
 * `node_modules/.pnpm/<pkg>/node_modules/<name>`, and only from that REAL
 * location are a package's own dependencies visible as siblings. Resolving from
 * the symlink path instead finds direct dependencies only.
 */
function resolvePackageDir(name, fromDir) {
  let dir = fromDir;
  while (dir.startsWith(root)) {
    const candidate = path.join(dir, 'node_modules', name);
    if (existsSync(path.join(candidate, 'package.json'))) {
      try {
        return realpathSync(candidate);
      } catch {
        return candidate;
      }
    }
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return null;
}

/** Normalise the many shapes of package.json `license` / `licenses`. */
function licenseOf(pkg) {
  if (typeof pkg.license === 'string' && pkg.license.trim()) return pkg.license.trim();
  // Legacy: { license: { type, url } }
  if (pkg.license && typeof pkg.license.type === 'string') return pkg.license.type.trim();
  // Legacy: { licenses: [{ type }, …] } — a real dual license, so report both
  // rather than silently picking one.
  if (Array.isArray(pkg.licenses)) {
    const types = pkg.licenses.map((l) => (typeof l === 'string' ? l : l?.type)).filter(Boolean);
    if (types.length) return types.join(' OR ');
  }
  return UNKNOWN;
}

/**
 * Turn a package.json `repository` (string shorthand, git+ssh URL, or object)
 * into a browsable https URL. Falls back to `homepage`, then to the npm page,
 * which always exists for a published package — better a correct npm link than
 * a fabricated repository URL that 404s.
 */
function repositoryUrl(pkg) {
  const repo = pkg.repository;
  let raw = typeof repo === 'string' ? repo : repo?.url;

  if (typeof raw === 'string' && raw.trim()) {
    raw = raw.trim().replace(/^git\+/, '').replace(/\.git$/, '');
    if (/^https?:\/\//.test(raw)) return raw.replace(/^http:\/\//, 'https://');
    if (raw.startsWith('git://')) return 'https://' + raw.slice('git://'.length);
    if (raw.startsWith('ssh://git@')) return 'https://' + raw.slice('ssh://git@'.length);
    // scp-like: git@github.com:owner/repo
    const scp = raw.match(/^git@([^:]+):(.+)$/);
    if (scp) return `https://${scp[1]}/${scp[2]}`;
    // Hosted shorthands: "owner/repo", "github:owner/repo", "gitlab:owner/repo"
    const shorthand = raw.match(/^(?:(github|gitlab|bitbucket):)?([\w.-]+\/[\w.-]+)$/);
    if (shorthand) {
      const hosts = { github: 'github.com', gitlab: 'gitlab.com', bitbucket: 'bitbucket.org' };
      return `https://${hosts[shorthand[1] ?? 'github']}/${shorthand[2]}`;
    }
  }

  if (typeof pkg.homepage === 'string' && /^https?:\/\//.test(pkg.homepage)) {
    return pkg.homepage.replace(/^http:\/\//, 'https://');
  }
  return `https://www.npmjs.com/package/${pkg.name}`;
}

function collect() {
  const rootPkg = readJson(path.join(root, 'package.json'));
  if (!rootPkg) throw new Error(`cannot read ${path.join(root, 'package.json')}`);

  const queue = Object.keys(rootPkg.dependencies ?? {}).map((name) => ({ name, from: root }));
  const visitedDirs = new Set();
  const byKey = new Map(); // "name@version" → entry (dedupes peer-dep duplicates)
  const missing = [];

  while (queue.length) {
    const { name, from } = queue.shift();
    const dir = resolvePackageDir(name, from);
    if (!dir) {
      missing.push(name);
      continue;
    }
    if (visitedDirs.has(dir)) continue;
    visitedDirs.add(dir);

    const pkg = readJson(path.join(dir, 'package.json'));
    if (!pkg?.name) continue;

    const version = typeof pkg.version === 'string' ? pkg.version : UNKNOWN;
    byKey.set(`${pkg.name}@${version}`, {
      name: pkg.name,
      version,
      license: licenseOf(pkg),
      url: repositoryUrl(pkg),
    });

    for (const dep of Object.keys(pkg.dependencies ?? {})) {
      queue.push({ name: dep, from: dir });
    }
  }

  return { packages: [...byKey.values()], missing };
}

const { packages, missing } = collect();
packages.sort((a, b) => a.name.localeCompare(b.name) || a.version.localeCompare(b.version));

const manifest = {
  generator: 'webinterface/scripts/gen-licenses.mjs (pnpm run gen:licenses)',
  note:
    'Runtime npm dependency closure. The license is the value declared by each package; ' +
    `"${UNKNOWN}" means the package declares none, not that it is unlicensed.`,
  count: packages.length,
  packages,
};

mkdirSync(path.dirname(outFile), { recursive: true });
writeFileSync(outFile, JSON.stringify(manifest, null, 2) + '\n');

const unknown = packages.filter((p) => p.license === UNKNOWN).length;
console.log(
  `gen-licenses: wrote ${path.relative(root, outFile)} (${packages.length} packages, ${unknown} without a declared license)`,
);
if (missing.length) {
  // Unresolvable dependencies mean the manifest is incomplete — loud, but not
  // fatal, so a partial install can still produce a usable file.
  console.warn(`gen-licenses: WARNING could not resolve: ${[...new Set(missing)].join(', ')}`);
}
