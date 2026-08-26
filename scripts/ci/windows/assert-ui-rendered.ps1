# ASCII ONLY. powershell.exe (5.1) decodes a BOM-less .ps1 as ANSI/CP1252, and
# PowerShell's tokenizer accepts the Unicode smart quotes that UTF-8 multi-byte
# sequences decode into -- so a single em dash inside a double-quoted string
# terminates that string early and the parse error surfaces a dozen lines later
# in unrelated code. This file only ever runs on a host that cannot be
# reproduced locally, so it must not depend on encoding at all.
#
# Proves the desktop UI actually RENDERED, by reading the live accessibility
# tree WebView2 exposes for its document.
#
# Why this and not a log line: 0.4.8 shipped a blank window to every desktop
# user (4579cb8a). Every cheaper signal stayed green through it -- the Rust
# side logged a clean boot, the `ui_boot_ok` handshake fired (it only proves
# the bundle's JS ran, not that React rendered anything), web-OTA rollback
# therefore never engaged, and the bridge/stores/pollers kept logging healthy
# traffic. The window was even shown, because show_window() runs in setup()
# regardless of what the webview ends up displaying. The only trace anywhere
# was one react-router line. So the gate has to assert the one thing that was
# actually false: the document has content.
#
# Why not CDP: wry unconditionally calls set_additional_browser_arguments
# (webview2/mod.rs), and WebView2 honours WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS
# only when that option is unset -- so --remote-debugging-port cannot be
# injected from outside without changing shipped config. Verified against
# wry 0.54.2 source, not assumed.
#
# The census counts descendants of the Document element only, never the
# window frame, so it cannot be satisfied by title-bar controls.
#
# Usage: assert-ui-rendered.ps1 -ProcessName <exe base name> [-TimeoutSec 120] [-MinNamed 5]

param(
  # The installed shell binary's base name. Passed in rather than hardcoded:
  # the Cargo package is k2app while productName is Kaitu, and guessing wrong
  # means Get-Process finds nothing and the failure reads as "no window" --
  # a harness miss wearing the costume of a product bug.
  [Parameter(Mandatory = $true)][string]$ProcessName,
  [int]$TimeoutSec = 120,
  [int]$MinNamed = 5
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes

$AE = [System.Windows.Automation.AutomationElement]
$Scope = [System.Windows.Automation.TreeScope]
$TrueCond = [System.Windows.Automation.Condition]::TrueCondition

function Get-AppWindow {
  $proc = Get-Process $ProcessName -ErrorAction SilentlyContinue | Select-Object -First 1
  if (-not $proc) { return $null }
  # Match on process id, not the window title: the title is localized
  # (a Chinese product name) and would silently stop matching on a copy edit.
  $cond = New-Object System.Windows.Automation.PropertyCondition($AE::ProcessIdProperty, $proc.Id)
  return $AE::RootElement.FindFirst($Scope::Children, $cond)
}

function Get-Document($win) {
  $ct = [System.Windows.Automation.ControlType]::Document
  $cond = New-Object System.Windows.Automation.PropertyCondition($AE::ControlTypeProperty, $ct)
  return $win.FindFirst($Scope::Descendants, $cond)
}

function Census($doc) {
  $all = $doc.FindAll($Scope::Descendants, $TrueCond)
  $named = @()
  foreach ($e in $all) {
    $n = $e.Current.Name
    if ($n -and $n.Trim().Length -gt 0) { $named += $n.Trim() }
  }
  return [pscustomobject]@{ Total = $all.Count; Named = $named }
}

$deadline = (Get-Date).AddSeconds($TimeoutSec)
$last = $null
$sawWindow = $false
$sawDocument = $false

while ((Get-Date) -lt $deadline) {
  # UIA elements go stale the moment the window they describe changes or
  # closes, and ErrorActionPreference=Stop would turn that into an opaque
  # crash instead of the diagnosis below. A stale read just means "try the
  # next tick".
  try {
    $win = Get-AppWindow
    if ($win) {
      $sawWindow = $true
      $doc = Get-Document $win
      if ($doc) {
        $sawDocument = $true
        $last = Census $doc
        Write-Host ("census: total={0} named={1}" -f $last.Total, $last.Named.Count)
        if ($last.Named.Count -ge $MinNamed) {
          Write-Host "UI RENDERED."
          Write-Host ("first named elements: {0}" -f (($last.Named | Select-Object -First 15) -join ' | '))
          exit 0
        }
      }
    }
  } catch [System.Windows.Automation.ElementNotAvailableException] {
    Write-Host "element went stale mid-read; retrying"
  } catch {
    Write-Host ("UIA read failed: {0}" -f $_.Exception.Message)
  }
  Start-Sleep -Seconds 3
}

Write-Host "=== FAIL ==="
if (-not $sawWindow) {
  Write-Host "No top-level window for process $ProcessName was ever found."
  Write-Host "The app may have exited, or never created its window."
} elseif (-not $sawDocument) {
  Write-Host "The window exists but WebView2 never exposed a Document element."
  Write-Host "Either the webview did not load anything, or its accessibility tree"
  Write-Host "is not being populated in this session -- the second case is a harness"
  Write-Host "problem and must be fixed rather than lowering -MinNamed."
} else {
  Write-Host ("The document exists but stayed effectively empty (total={0} named={1}, want named>={2})." -f `
    $last.Total, $last.Named.Count, $MinNamed)
  Write-Host "This is the 0.4.8 blank-window shape: the app 'started fine' and every"
  Write-Host "log stayed healthy while the user saw nothing."
  if ($last.Named.Count -gt 0) {
    Write-Host ("names seen: {0}" -f ($last.Named -join ' | '))
  }
}
exit 1
