#!/usr/bin/env node
// Thin launcher: finds the Go binary for this OS/CPU and runs it.
// Node only installs and starts it; the agent itself has no runtime dependency.
"use strict";

const { spawnSync } = require("child_process");

const PACKAGES = {
  "win32 x64": "@db-agent/win32-x64",
  "win32 arm64": "@db-agent/win32-arm64",
  "darwin x64": "@db-agent/darwin-x64",
  "darwin arm64": "@db-agent/darwin-arm64",
  "linux x64": "@db-agent/linux-x64",
  "linux arm64": "@db-agent/linux-arm64",
};

function findBinary() {
  if (process.env.DB_AGENT_BINARY) return process.env.DB_AGENT_BINARY; // dev override

  const key = `${process.platform} ${process.arch}`;
  const pkg = PACKAGES[key];
  if (!pkg) {
    console.error(`db-agent: unsupported platform "${key}".`);
    process.exit(1);
  }
  const exe = process.platform === "win32" ? "db-agent.exe" : "db-agent";
  try {
    return require.resolve(`${pkg}/bin/${exe}`);
  } catch {
    console.error(
      `db-agent: the package "${pkg}" is missing.\n` +
        "Reinstall without --no-optional / --omit=optional."
    );
    process.exit(1);
  }
}

const result = spawnSync(findBinary(), process.argv.slice(2), {
  stdio: "inherit", // stdin/stdout pass straight through (needed for MCP)
  windowsHide: true,
  env: { ...process.env, DB_AGENT_LAUNCHER: "npm" },
});

if (result.error) {
  console.error(`db-agent: ${result.error.message}`);
  process.exit(1);
}
if (result.signal) process.kill(process.pid, result.signal);
process.exit(result.status ?? 1);
