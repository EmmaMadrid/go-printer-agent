; ─────────────────────────────────────────────────────────────────────────────
; Instalador gráfico del agente de impresión (Inno Setup 6).
;
; Genera un único .exe de "siguiente-siguiente-terminar" que copia el agente,
; lo registra para arrancar solo y lo deja corriendo. Aparece en "Aplicaciones
; instaladas" y se desinstala desde ahí.
;
; Compilar:  powershell -File build.ps1 -Installer
; ─────────────────────────────────────────────────────────────────────────────

#ifndef MyAppVersion
  #define MyAppVersion "2.0.0"
#endif

#define MyAppName      "Sait Printer Agent"
#define MyAppPublisher "Sait Restaurantes"
#define MyAppExe       "printer-agent.exe"

[Setup]
AppId={{8F3C2E71-4A6B-4E2D-9C57-1E6B0A9D3F52}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={autopf}\SaitPrinterAgent
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
OutputDir=Output
OutputBaseFilename=SaitPrinterAgent-{#MyAppVersion}
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
; El servicio se instala en Archivos de programa y toca el registro de
; servicios: hace falta elevación.
PrivilegesRequired=admin
ArchitecturesInstallIn64BitMode=x64compatible
UninstallDisplayIcon={app}\{#MyAppExe}

[Languages]
Name: "es"; MessagesFile: "compiler:Languages\Spanish.isl"

[Types]
Name: "servicio"; Description: "Servicio de Windows (recomendado para cajas fijas)"
Name: "sesion";   Description: "Tarea al iniciar sesión (ve impresoras del usuario y de Escritorio Remoto)"

[Components]
Name: "agente"; Description: "Agente de impresión"; Types: servicio sesion; Flags: fixed
Name: "pdf";    Description: "Ayudante para imprimir PDF a una impresora específica"; Types: servicio sesion

[Files]
Source: "..\release\printer-agent.exe";  DestDir: "{app}"; Flags: ignoreversion; Components: agente
Source: "..\release\README.md";          DestDir: "{app}"; Flags: ignoreversion; Components: agente
; La configuración de la estación no se pisa al actualizar: cada caja ya eligió
; su impresora desde el POS.
Source: "..\release\config.example.json"; DestDir: "{app}"; DestName: "config.json"; Flags: onlyifdoesntexist uninsneveruninstall; Components: agente
Source: "..\release\bin\*";               DestDir: "{app}\bin"; Flags: ignoreversion skipifsourcedoesntexist; Components: pdf

[Icons]
Name: "{group}\Estado del agente"; Filename: "{cmd}"; Parameters: "/k ""{app}\{#MyAppExe}"" status"; Comment: "Muestra el estado del agente y sus impresoras"
Name: "{group}\Desinstalar {#MyAppName}"; Filename: "{uninstallexe}"

[Run]
; Antes de registrar nada, se limpia cualquier instalación previa (permite
; actualizar y cambiar de modo servicio a modo sesión sin dejar restos).
Filename: "{app}\{#MyAppExe}"; Parameters: "uninstall";      Flags: runhidden waituntilterminated; StatusMsg: "Quitando la versión anterior..."
Filename: "{app}\{#MyAppExe}"; Parameters: "uninstall-user"; Flags: runhidden waituntilterminated; StatusMsg: "Quitando la versión anterior..."

Filename: "{app}\{#MyAppExe}"; Parameters: "install";      Flags: runhidden waituntilterminated; StatusMsg: "Registrando el servicio..."; Check: EsTipo('servicio')
Filename: "{app}\{#MyAppExe}"; Parameters: "install-user"; Flags: runhidden waituntilterminated; StatusMsg: "Registrando el arranque de sesión..."; Check: EsTipo('sesion')

[UninstallRun]
Filename: "{app}\{#MyAppExe}"; Parameters: "uninstall";      Flags: runhidden waituntilterminated; RunOnceId: "QuitarServicio"
Filename: "{app}\{#MyAppExe}"; Parameters: "uninstall-user"; Flags: runhidden waituntilterminated; RunOnceId: "QuitarTarea"

[UninstallDelete]
Type: files; Name: "{app}\printer-agent.log"
Type: files; Name: "{app}\printer-agent.log.old"

[Code]
// EsTipo permite condicionar los pasos de [Run] al modo elegido en el asistente.
function EsTipo(Nombre: String): Boolean;
begin
  Result := (WizardSetupType(False) = Nombre);
end;
