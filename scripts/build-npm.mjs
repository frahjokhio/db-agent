// Builds the npm packages into dist/npm/:
//   dist/npm/cli            -> @db-agent/cli (launcher)
//   dist/npm/<os>-<cpu>     -> @db-agent/<os>-<cpu> (Go binary)
//
// Usage: node scripts/build-npm.mjs [version]
// Publish (later): publish every platform package first, then cli. See README.
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const cliTemplate = JSON.parse(fs.readFileSync(path.join(root, "npm/cli/package.json"), "utf8"));
const version = process.argv[2] ?? cliTemplate.version;

const targets = [
  { name: "win32-x64", goos: "windows", goarch: "amd64", os: "win32", cpu: "x64" },
  { name: "win32-arm64", goos: "windows", goarch: "arm64", os: "win32", cpu: "arm64" },
  { name: "darwin-x64", goos: "darwin", goarch: "amd64", os: "darwin", cpu: "x64" },
  { name: "darwin-arm64", goos: "darwin", goarch: "arm64", os: "darwin", cpu: "arm64" },
  { name: "linux-x64", goos: "linux", goarch: "amd64", os: "linux", cpu: "x64" },
  { name: "linux-arm64", goos: "linux", goarch: "arm64", os: "linux", cpu: "arm64" },
];

const out = path.join(root, "dist", "npm");
fs.rmSync(out, { recursive: true, force: true });

const writeJSON = (file, data) => fs.writeFileSync(file, JSON.stringify(data, null, 2) + "\n");

for (const t of targets) {
  const dir = path.join(out, t.name);
  const exe = t.goos === "windows" ? "db-agent.exe" : "db-agent";
  console.log(`building ${t.name} ...`);
  execFileSync(
    "go",
    ["build", "-trimpath", "-ldflags", `-s -w -X github.com/frahjokhio/db-agent/internal/cli.Version=${version}`,
      "-o", path.join(dir, "bin", exe), "./cmd/db-agent"],
    { cwd: root, stdio: "inherit", env: { ...process.env, GOOS: t.goos, GOARCH: t.goarch, CGO_ENABLED: "0" } }
  );
  if (t.goos !== "windows") fs.chmodSync(path.join(dir, "bin", exe), 0o755);
  fs.copyFileSync(path.join(root, "LICENSE"), path.join(dir, "LICENSE"));
  writeJSON(path.join(dir, "package.json"), {
    name: `@db-agent/${t.name}`,
    version,
    description: `db-agent binary for ${t.os} ${t.cpu}. Install @db-agent/cli instead.`,
    license: cliTemplate.license,
    homepage: cliTemplate.homepage,
    repository: cliTemplate.repository,
    os: [t.os],
    cpu: [t.cpu],
    files: ["bin", "LICENSE"],
    preferUnplugged: true,
    publishConfig: { access: "public" },
  });
}

const cliDir = path.join(out, "cli");
fs.mkdirSync(path.join(cliDir, "bin"), { recursive: true });
fs.copyFileSync(path.join(root, "npm/cli/bin/db-agent.js"), path.join(cliDir, "bin/db-agent.js"));
fs.copyFileSync(path.join(root, "README.md"), path.join(cliDir, "README.md"));
fs.copyFileSync(path.join(root, "LICENSE"), path.join(cliDir, "LICENSE"));
writeJSON(path.join(cliDir, "package.json"), {
  ...cliTemplate,
  version,
  optionalDependencies: Object.fromEntries(targets.map((t) => [`@db-agent/${t.name}`, version])),
});

console.log(`\nOK: npm packages for v${version} in ${out}`);
