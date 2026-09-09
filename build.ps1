# ─────────────────────────────────────────────────────────────────────────────
# Compila el agente y arma la carpeta release\ lista para distribuir.
#
# Uso:  powershell -ExecutionPolicy Bypass -File build.ps1 [-Version 2.0.0] [-Installer]
#
# Requisitos: Go 1.22 o superior. Nada más: no hay que descargar herramientas al
# vuelo ni empaquetar un runtime. Con -Installer se genera además el .exe de
# instalación, si Inno Setup está disponible.
# ─────────────────────────────────────────────────────────────────────────────
param(
  [string]$Version = "2.0.0",
  [switch]$Installer
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

# Si el agente está corriendo, el .exe queda bloqueado y no se puede reemplazar.
$corriendo = Get-Process -Name 'printer-agent' -ErrorAction SilentlyContinue
if ($corriendo) {
  Write-Host 'Deteniendo el agente en ejecución para poder recompilar...' -ForegroundColor Yellow
  $corriendo | Stop-Process -Force -ErrorAction SilentlyContinue
  Start-Sleep -Milliseconds 500
}

Write-Host '1/4  Verificando (vet + pruebas)...' -ForegroundColor Cyan
go vet ./...
if ($LASTEXITCODE -ne 0) { throw 'go vet encontró problemas' }
go test ./...
if ($LASTEXITCODE -ne 0) { throw 'las pruebas fallaron' }

Write-Host '2/4  Compilando...' -ForegroundColor Cyan
# -H windowsgui: sin consola parpadeante al arrancar por tarea de sesión; los
#                subcomandos se enganchan a la terminal por su cuenta.
# -s -w:         sin tabla de símbolos ni DWARF, ~30 % menos de tamaño.
$ldflags = "-H windowsgui -s -w -X github.com/EmmaMadrid/go-printer-agent/internal/httpapi.Version=$Version"
New-Item -ItemType Directory -Force -Path dist | Out-Null
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
go build -trimpath -ldflags $ldflags -o dist\printer-agent.exe .\cmd\agent
if ($LASTEXITCODE -ne 0) { throw 'falló la compilación' }

Write-Host '3/4  Armando release\...' -ForegroundColor Cyan
$rel = 'release'
Remove-Item -Recurse -Force $rel -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path "$rel\bin" | Out-Null
Copy-Item 'dist\printer-agent.exe' "$rel\" -Force
Copy-Item 'config.example.json'    "$rel\" -Force
Copy-Item 'README.md'              "$rel\" -Force
# Ayudante para imprimir PDF a una impresora con nombre (opcional pero
# recomendado). El release NO lleva config.json: cada estación elige su
# impresora desde el POS.
if (Test-Path 'bin') { Copy-Item 'bin\*' "$rel\bin\" -Force -ErrorAction SilentlyContinue }

$mb = [math]::Round((Get-Item "$rel\printer-agent.exe").Length / 1MB, 1)
Write-Host "     printer-agent.exe = $mb MB" -ForegroundColor DarkGray

Write-Host '4/4  Instalador...' -ForegroundColor Cyan
if ($Installer) {
  $iscc = @(
    "$env:ProgramFiles\Inno Setup 6\ISCC.exe",
    "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe"
  ) | Where-Object { Test-Path $_ } | Select-Object -First 1

  if (-not $iscc) {
    Write-Host '     Inno Setup no está instalado; se omite el instalador.' -ForegroundColor Yellow
    Write-Host '     Descárgalo de https://jrsoftware.org/isdl.php y vuelve a correr con -Installer.' -ForegroundColor Yellow
  } else {
    & $iscc "/DMyAppVersion=$Version" 'installer\setup.iss'
    if ($LASTEXITCODE -ne 0) { throw 'falló la generación del instalador' }
    Write-Host "     installer\Output\SaitPrinterAgent-$Version.exe" -ForegroundColor DarkGray
  }
} else {
  Write-Host '     (omitido; usa -Installer para generarlo)' -ForegroundColor DarkGray
}

Write-Host ''
Write-Host "LISTO -> $rel\" -ForegroundColor Green
Write-Host 'Para instalar en una caja: copia release\ y ejecuta' -ForegroundColor Green
Write-Host '  printer-agent.exe install        (servicio de Windows, pide permiso de administrador)' -ForegroundColor Green
Write-Host '  printer-agent.exe install-user   (tarea de inicio de sesión, sin administrador)' -ForegroundColor Green
