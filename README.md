# printer-agent (Go)

Un **microservicio local de impresión**: hace de puente entre una app web (local o
en la nube) y las impresoras físicas de una PC — **térmicas ESC/POS** (tickets de
58/80 mm, corte de papel, cajón de dinero) e impresoras de **página completa**
(láser/inyección: facturas, reportes).

Tu app solo hace `POST` de un **JSON** a `http://localhost:9911`; el agente se
encarga del hardware. Es **agnóstico del dominio**, así que se reutiliza en
cualquier proyecto.

```
┌───────────────────────────┐   POST    ┌──────────────────────────────────────┐
│  Tu app (navegador)        │   JSON    │  PC local — printer-agent.exe         │
│  cloud o local             │──────────▶│  escucha SOLO en 127.0.0.1:9911       │
│                            │           │                                       │
│  fetch localhost:9911/print│           │  /print ─┬ "bloques"   → ESC/POS ─────┼─▶ 🖨️ térmica 58/80mm
│                            │◀──────────│          └ "documento" → HTML→PDF ────┼─▶ 🖨️ página completa
│  si NO responde →          │  { ok }   │  /pdf   → HTML→PDF (devuelve bytes)   │
│  window.print() (fallback) │           │  /status  /health  /printers          │
└───────────────────────────┘           └──────────────────────────────────────┘
```

Es la **reescritura en Go** del agente original en Node
([js-printer-agent](https://github.com/EmmaMadrid/js-printer-agent)). Habla
exactamente el mismo protocolo: se cambia un ejecutable por el otro sin tocar
una línea de la app.

## Por qué la versión en Go

El agente de Node funcionaba bien; lo que dolía era **instalarlo en cada
máquina**. Esta versión ataca justo eso:

| | Node (SEA) | Go |
| --- | --- | --- |
| Tamaño del ejecutable | ~91 MB (runtime de Node embebido) | **~8 MB** |
| Archivos a copiar | `.exe` + `rawprint.ps1` + `run-hidden.vbs` + 8 scripts | **1 solo `.exe`** |
| Impresión RAW | PowerShell → compila C# al vuelo → P/Invoke | llamada directa a `winspool.drv` |
| Listar impresoras | lanza `powershell Get-Printer` por cada consulta | `EnumPrinters` en proceso |
| Estado de la impresora | lanza `powershell Get-Printer` | `GetPrinter` en proceso |
| Servicio de Windows | **descarga WinSW de internet** | nativo (`agent install`) |
| Política de ejecución de scripts | hay que sortearla con `-ExecutionPolicy Bypass` | no se usan scripts |
| Instalación | copiar carpeta + elegir entre 4 `.cmd` | un `.exe` de instalación, o un subcomando |

Además: cola por impresora (dos tickets simultáneos ya no se entrelazan),
reintentos ante fallos pasajeros y un endpoint `/health` que dice exactamente
qué falta cuando "no sale el ticket".

---

## Instalación

### Con el instalador gráfico (recomendado)

Ejecuta `SaitPrinterAgent-<versión>.exe` y elige el modo:

- **Servicio de Windows** — arranca con el equipo, antes de iniciar sesión.
  Ideal para cajas fijas. Ojo: un servicio corre como *LocalSystem* y **no ve**
  las impresoras instaladas solo para tu usuario ni las redirigidas por
  Escritorio Remoto; para esas, usa el otro modo o una impresora de red (`conexion: "red"`).
- **Tarea al iniciar sesión** — corre en la sesión del usuario y ve *todas* sus
  impresoras, incluidas las de RDP.

Aparece en *Aplicaciones instaladas* y se quita desde ahí. Al desinstalar, el
`config.json` de la estación se conserva.

### Sin instalador (copiar y ejecutar)

```bat
printer-agent.exe install        :: servicio de Windows (pide permiso de administrador)
printer-agent.exe install-user   :: tarea de inicio de sesión (sin administrador)
printer-agent.exe run            :: en primer plano, para probar
```

### Subcomandos

| Subcomando | Qué hace |
| --- | --- |
| `run` | Corre el agente en primer plano (por defecto). |
| `install` / `uninstall` | Registra o quita el servicio de Windows. Se autoeleva. |
| `start` / `stop` | Arranca o detiene el servicio ya instalado. |
| `install-user` / `uninstall-user` | Tarea de inicio de sesión, sin administrador. |
| `status` | Estado del servicio, de la configuración y de las impresoras. |
| `printers` | Lista las impresoras instaladas (marca la predeterminada). |
| `version` | Versión del agente. |

Banderas: `-config <ruta>`, `-port <n>`, `-host <ip>`.

### Actualizar una estación

Vuelve a correr el instalador: quita el registro anterior, reemplaza el `.exe` y
lo deja corriendo, conservando el `config.json`. A mano:

```bat
printer-agent.exe stop
copy /Y nueva\printer-agent.exe "C:\Program Files\SaitPrinterAgent\"
printer-agent.exe start
```

---

## Compilar

```powershell
powershell -ExecutionPolicy Bypass -File build.ps1                    # release\
powershell -ExecutionPolicy Bypass -File build.ps1 -Installer         # release\ + instalador
```

**Requisitos:** Go 1.22+. Para el instalador, además
[Inno Setup 6](https://jrsoftware.org/isdl.php). No se descarga nada al vuelo.

`build.ps1` corre `go vet` y las pruebas antes de compilar, así que un cambio que
altere la salida de un ticket no llega al `.exe`.

---

## API

| Método | Ruta | Descripción |
| --- | --- | --- |
| `GET` | `/status` | ¿Vivo? Impresoras y roles configurados. |
| `GET` | `/health` | Diagnóstico: enumera lo que impediría imprimir. |
| `GET` | `/version` | Versión del agente y del runtime. |
| `GET` | `/printers` | Impresoras instaladas en Windows (para armar un selector). |
| `GET` | `/printers/network` | Escanea la LAN buscando térmicas en el puerto 9100. |
| `POST` | `/print` | Imprime. El cuerpo define el formato (ver abajo). |
| `POST` | `/pdf` | Convierte HTML → PDF y **devuelve los bytes**. |

Respuesta de `/print`: `{ "ok": true, ... }` o `{ "ok": false, "error": "…" }`
(el agente traduce fallos: impresora fuera de línea, no instalada, sin papel,
tapa abierta, en pausa…).

`/health` es lo primero que hay que mirar cuando alguien reporta que no imprime:

```jsonc
{
  "ok": false,
  "notas": { "conexion": "usb", "impresora": "BIXOLON SRP-330", "estado": "PaperOut", … },
  "carta": { "impresora": "(predeterminada)", "navegador": "C:\\…\\msedge.exe", "pdfTool": null },
  "impresorasInstaladas": 5,
  "problemas": ["La impresora \"BIXOLON SRP-330\" se quedó sin papel."]
}
```

---

## Contratos de impresión

### 1) Ticket térmico por **bloques** (agnóstico del dominio)

Describes el ticket como una lista de bloques; el agente **no sabe** de
restaurantes ni tiendas, solo renderiza lo que le mandes.

```jsonc
POST /print
{
  "bloques": [
    { "t": "logo", "data": "data:image/png;base64,…" },   // se difumina a 1-bit solo
    { "t": "txt", "v": "MI EMPRESA", "align": "center", "bold": true, "grande": true },
    { "t": "hr" },
    { "t": "kv", "l": "Folio", "r": "A-001" },
    { "t": "cols", "items": [
      { "v": "2", "w": 0.15 },
      { "v": "Producto de ejemplo", "w": 0.55 },
      { "v": "$120.00", "w": 0.30, "align": "right" }
    ]},
    { "t": "kv", "l": "TOTAL", "r": "$374.00", "bold": true },
    { "t": "feed", "n": 1 },
    { "t": "txt", "v": "¡Gracias por su compra!", "align": "center" }
  ],
  "ancho": 42,                       // columnas (80mm ≈ 42–48; 58mm ≈ 32)
  "opciones": { "cortar": true, "abrirCajon": false },
  "impresora": { "conexion": "usb", "printerName": "EPSON TM-T20" }
}
```

| `t` | Campos | Qué hace |
| --- | --- | --- |
| `txt` | `v`, `align`, `bold`, `grande` | Línea de texto (se dobla al ancho del papel). |
| `kv` | `l`, `r`, `bold` | Etiqueta a la izquierda, valor a la derecha. |
| `cols` | `items: [{ v, w?, cols?, align?, bold? }]` | Fila de columnas. `w` = ancho relativo (0–1); `cols` = columnas exactas. |
| `hr` | — | Línea divisoria a lo ancho. |
| `feed` | `n` | Avanza `n` líneas en blanco. |
| `logo`/`img` | `data` (data-URL) o `v`; `ancho` | Imagen: se reduce, se pasa a gris y se difumina a 1-bit. |

### 2) Documento de **página completa** (HTML → PDF → impresora)

```jsonc
POST /print
{
  "tipo": "documento",
  "html": "<!DOCTYPE html><html>… tu documento …</html>",
  "printerName": "HP LaserJet",      // opcional; vacío = predeterminada
  "paper": "letter"
}
```

El tamaño lo controla el `@page { size: letter }` de tu HTML.

### 3) Obtener un PDF (descargar / adjuntar a correo)

```jsonc
POST /pdf
{ "html": "<!DOCTYPE html>…" }       // → responde el PDF (application/pdf)
```

### El objeto `impresora`

```jsonc
"impresora": {
  "conexion": "usb",                 // "usb" (nombre de Windows) | "red" (TCP)
  "printerName": "EPSON TM-T20",
  "red": { "host": "192.168.1.50", "port": 9100 },
  "type": "epson",                   // epson | star (dialecto ESC/POS)
  "width": 42, "characterSet": "PC858_EURO"
}
```

- **`usb`** → los bytes van en RAW al spooler de Windows, por nombre.
- **`red`** → socket TCP directo a la IP de la impresora. Es lo indicado cuando
  el agente corre como servicio: no depende de que la impresora esté instalada
  en el perfil del usuario.

Lo que omitas se hereda del `config.json` de la estación.

### Plantillas del proyecto de referencia

El agente incluye además tres plantillas del POS que lo originó — `tipo:"comanda"`
(orden de cocina), `tipo:"kiosko"` (ticket de autoservicio) y el ticket por
`lineas` (nota de venta). **Un proyecto nuevo no las usa**: imprime con `bloques`
y `documento`, que son 100 % agnósticos.

---

## Integración desde tu app

```js
const AGENTE = 'http://localhost:9911'

const vivo = await fetch(`${AGENTE}/status`).then(r => r.ok).catch(() => false)
const { printers } = await fetch(`${AGENTE}/printers`).then(r => r.json())

async function imprimir(payload) {
  try {
    const r = await fetch(`${AGENTE}/print`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
    if (r.ok) return { ok: true, via: 'agente' }
    return { ok: false, error: (await r.json()).error }
  } catch {
    window.print()                    // sin agente → impresión del navegador
    return { ok: true, via: 'navegador' }
  }
}
```

**Patrón recomendado:** guarda la impresora elegida **por dispositivo** (en el
`localStorage` de esa PC) y mándala en el bloque `impresora` de cada trabajo; así
el agente queda genérico y cada estación imprime a lo suyo sin editar archivos.

---

## Configuración (`config.json`)

Opcional — solo actúa como respaldo si tu app **no** manda el bloque `impresora`.
Se relee en cada petición (no requiere reiniciar). Vive junto al ejecutable; si
ahí no está, se busca en el directorio actual, y con `-config` se indica a mano.

```jsonc
{
  "port": 9911,
  "host": "127.0.0.1",               // "0.0.0.0" para aceptar la LAN (tablet del kiosko)
  "origenes": "*",                   // "*" o el dominio de tu app (CORS)
  "notas": {
    "type": "epson", "conexion": "usb", "printerName": "",
    "red": { "host": "", "port": 9100 },
    "width": 42, "characterSet": "PC858_EURO",
    "cortar": true, "abrirCajon": true, "logoPath": "", "logoAnchoDots": 384
  },
  "carta": { "printerName": "", "paper": "letter" },
  "pdfTool": ""                      // vacío = autodetecta bin/
}
```

Juegos de caracteres disponibles: `PC858_EURO` (por defecto, cubre el español),
`WPC1252`, `PC850_MULTILINGUAL`, `PC852_LATIN2`, `PC437_USA` y varios más.

---

## Seguridad

- Escucha **solo en `127.0.0.1`** salvo que se cambie `host` → ningún equipo
  externo lo alcanza; solo el navegador de la propia PC.
- **CORS** configurable por `origenes`.
- Manda la cabecera **`Access-Control-Allow-Private-Network`**, que Chrome y Edge
  exigen para un salto de red pública a red local. Requisito: tu app debe
  servirse por **HTTPS**.
- El cuerpo de una petición está limitado a 8 MB.

---

## Requisitos de la ruta "documento" (página completa)

- **Microsoft Edge o Chrome** (viene con Windows) para renderizar HTML → PDF.
- Para imprimir el PDF a una impresora **específica**: `SumatraPDF.exe` o
  `PDFtoPrinter.exe` en la carpeta `bin/` del agente. Sin ese ayudante solo se
  puede imprimir a la **predeterminada** de Windows.

---

## Equivalencia con el agente de Node

Las cuatro plantillas térmicas producen **bytes idénticos** a los del agente JS.
La prueba está automatizada: `internal/render/testdata/*.bin` son capturas
**reales** del agente de Node —obtenidas haciéndolo imprimir a un socket local
con `tools/capturar.js`— y `go test ./...` compara byte a byte.

```bash
# Levantar el agente JS y capturar su salida
cd ../sait-rest/printer-agent && PORT=9921 node index.js &
node tools/capturar.js http://127.0.0.1:9921 internal/render/testdata
go test ./...
```

Diferencia conocida y deliberada: al **reducir un logotipo** grande, Jimp usaba
interpolación bilineal y aquí se promedia por cajas. La figura, el tamaño y la
cobertura de tinta son equivalentes (0.04 % de diferencia medida), y promediar
conserva más detalle antes del difuminado.

---

## Solución de problemas

Primero: `printer-agent.exe status` o `GET /health`. Casi siempre ahí está la
respuesta.

- **Tu app dice "sin agente":** no está corriendo o el puerto no coincide. Abre
  `http://localhost:9911/status` en el navegador.
- **Térmica imprime "basura":** casi siempre es una imagen mal formada; usa el
  bloque `logo`/`img` (el agente la procesa) en vez de mandar bytes crudos. Si es
  texto, revisa `characterSet` (`PC858_EURO` o `WPC1252` para acentos).
- **Se desborda el ancho:** baja `ancho` (42 → 40) según el modelo.
- **`La impresora "…" no está instalada`:** el nombre no coincide; cópialo de
  `/printers` o de `printer-agent.exe printers`.
- **Carta a impresora específica no imprime:** falta `SumatraPDF.exe` /
  `PDFtoPrinter.exe` en `bin/`.
- **El servicio no ve la impresora:** corre como *LocalSystem* y no ve las
  impresoras del perfil de un usuario → usa `conexion: "red"` (por IP), instala
  la impresora para todos los usuarios, o cambia a `install-user`.
