# Publishes dist/npm/* to npm: the 6 platform packages first, then @db-agent/cli.
# Run `node scripts/build-npm.mjs <version>` and `npm login` first.
# Safe to re-run: versions already on npm are skipped.
#
# Note: npm's normal "not found" answers go to stderr. Windows PowerShell 5 turns
# stderr into errors, so npm checks run through cmd and we test exit codes instead.
$root = Split-Path -Parent $PSScriptRoot
$order = "win32-x64", "win32-arm64", "darwin-x64", "darwin-arm64", "linux-x64", "linux-arm64", "cli"

$user = cmd /c "npm whoami 2>nul"
if ($LASTEXITCODE -ne 0 -or -not $user) { Write-Host "Not logged in. Run: npm login"; exit 1 }
Write-Host "Logged in as: $user"

foreach ($p in $order) {
    $dir = Join-Path $root "dist\npm\$p"
    $pkg = Get-Content (Join-Path $dir "package.json") | ConvertFrom-Json
    $id = "$($pkg.name)@$($pkg.version)"

    $existing = cmd /c "npm view $id version 2>nul"
    if ($LASTEXITCODE -eq 0 -and "$existing".Trim() -eq $pkg.version) {
        Write-Host "skip $id (already published)"
        continue
    }

    Write-Host "`n=== publishing $id ===  (approve with your security key if asked)"
    Push-Location $dir
    cmd /c "npm publish --access public"
    $code = $LASTEXITCODE
    Pop-Location
    if ($code -ne 0) {
        Write-Host "`nFAILED: $id (exit $code). Fix the error above, then run this script again."
        exit $code
    }
}
Write-Host "`nOK: all packages published. Test: npx -y @db-agent/cli version"
