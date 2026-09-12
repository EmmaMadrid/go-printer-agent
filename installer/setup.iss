; ─────────────────────────────────────────────────────────────────────────────
; Instalador gráfico del agente de impresión (Inno Setup 6).
;
; Genera un único .exe de "siguiente-siguiente-terminar" que copia el agente,
; lo registra para arrancar solo y lo deja corriendo. Aparece en "Aplicaciones
; instaladas" y se desinstala desde ahí.
;
; Migra solo desde el agente anterior (Node + WinSW): detiene y elimina su
; servicio, borra sus archivos y conserva el config.json de la estación, que
; tiene el mismo formato. Compilar:  powershell -File build.ps1 -Installer
; ─────────────────────────────────────────────────────────────────────────────

#ifndef MyAppVersion
  #define MyAppVersion "2.0.0"
#endif

#define MyAppName      "Sait Printer Agent"
#define MyAppPublisher "Sait Restaurantes"
#define MyAppExe       "printer-agent.exe"
#define ServiceId      "SaitPrinterAgent"

[Setup]
AppId={{8F3C2E71-4A6B-4E2D-9C57-1E6B0A9D3F52}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
; Misma carpeta que usaba el agente de Node, para heredar su config.json.
DefaultDirName={autopf}\SaitPrinterAgent
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
OutputDir=Output
OutputBaseFilename=SaitPrinterAgent-{#MyAppVersion}
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
; Se toca el registro de servicios y Archivos de programa: hace falta elevación.
PrivilegesRequired=admin
ArchitecturesInstallIn64BitMode=x64compatible
UninstallDisplayIcon={app}\{#MyAppExe}
CloseApplications=no

[Languages]
Name: "es"; MessagesFile: "compiler:Languages\Spanish.isl"

[Types]
Name: "servicio"; Description: "Servicio de Windows (recomendado para cajas fijas)"
Name: "sesion";   Description: "Tarea al iniciar sesión (ve impresoras del usuario y de Escritorio Remoto)"

[Components]
Name: "agente"; Description: "Agente de impresión"; Types: servicio sesion; Flags: fixed
Name: "pdf";    Description: "Ayudante para imprimir PDF a una impresora específica"; Types: servicio sesion

[InstallDelete]
; Restos del agente de Node. Para cuando se llega aquí, PrepareToInstall ya
; detuvo el servicio viejo y los archivos están libres.
Type: files; Name: "{app}\sait-printer-agent.exe"
Type: files; Name: "{app}\SaitPrinterAgentService.exe"
Type: files; Name: "{app}\SaitPrinterAgentService.xml"
Type: files; Name: "{app}\SaitPrinterAgentService.*.log"
Type: files; Name: "{app}\PrinterAgentService.exe"
Type: files; Name: "{app}\PrinterAgentService.xml"
Type: files; Name: "{app}\PrinterAgentService.*.log"
Type: files; Name: "{app}\rawprint.ps1"
Type: files; Name: "{app}\run-hidden.vbs"
Type: files; Name: "{app}\*.ps1"
Type: files; Name: "{app}\*.cmd"

[Files]
Source: "..\release\printer-agent.exe";  DestDir: "{app}"; Flags: ignoreversion; Components: agente
Source: "..\release\README.md";          DestDir: "{app}"; Flags: ignoreversion; Components: agente
; La configuración de la estación no se pisa: si ya hay config.json (del
; agente viejo o de una versión anterior de este) se respeta tal cual.
Source: "..\release\config.example.json"; DestDir: "{app}"; DestName: "config.json"; Flags: onlyifdoesntexist uninsneveruninstall; Components: agente
Source: "..\release\config.example.json"; DestDir: "{app}"; Flags: ignoreversion; Components: agente
Source: "..\release\bin\*";               DestDir: "{app}\bin"; Flags: ignoreversion skipifsourcedoesntexist; Components: pdf

[Icons]
Name: "{group}\Estado del agente"; Filename: "{cmd}"; Parameters: "/k ""{app}\{#MyAppExe}"" status"; Comment: "Muestra el estado del agente y sus impresoras"
Name: "{group}\Desinstalar {#MyAppName}"; Filename: "{uninstallexe}"

[Run]
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

// Ejecuta un comando en silencio y devuelve su código de salida (-1 si no
// se pudo lanzar). Los fallos aquí no son fatales: si algo no existía, mejor.
function Correr(Exe, Args: String): Integer;
var
  Codigo: Integer;
begin
  if Exec(Exe, Args, '', SW_HIDE, ewWaitUntilTerminated, Codigo) then
    Result := Codigo
  else
    Result := -1;
end;

// ServicioDetenido consulta al SCM; devuelve True si el servicio está STOPPED
// o ya no existe (sc reporta 1060).
function ServicioDetenido(Id: String): Boolean;
begin
  Result := Correr(ExpandConstant('{cmd}'),
    '/c sc query ' + Id + ' | findstr /C:"STOPPED" /C:"1060" >nul') = 0;
end;

// QuitarAgentePrevio deja la máquina limpia antes de copiar archivos: detiene
// y elimina cualquier servicio con el mismo id (sea el WinSW del agente de Node
// o una versión anterior de este), mata procesos sueltos y quita tareas de
// sesión antiguas. Así el .exe viejo queda libre para [InstallDelete] y el
// registro del servicio nuevo no choca.
procedure QuitarAgentePrevio;
var
  Intentos: Integer;
begin
  Correr('sc.exe', 'stop {#ServiceId}');
  Intentos := 0;
  while (Intentos < 30) and (not ServicioDetenido('{#ServiceId}')) do
  begin
    Sleep(500);
    Intentos := Intentos + 1;
  end;
  Correr('sc.exe', 'delete {#ServiceId}');
  Correr('sc.exe', 'stop PrinterAgent');
  Correr('sc.exe', 'delete PrinterAgent');

  Correr('taskkill.exe', '/F /IM sait-printer-agent.exe');
  Correr('taskkill.exe', '/F /IM printer-agent.exe');
  Correr('taskkill.exe', '/F /IM SaitPrinterAgentService.exe');
  Correr('taskkill.exe', '/F /IM PrinterAgentService.exe');

  Correr('schtasks.exe', '/Delete /TN {#ServiceId} /F');
  Correr('schtasks.exe', '/Delete /TN PrinterAgent /F');

  // Un instante para que el SCM y el sistema de archivos suelten los handles.
  Sleep(800);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  QuitarAgentePrevio;
  Result := '';
end;
