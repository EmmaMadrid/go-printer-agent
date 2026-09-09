// Captura los bytes ESC/POS que un agente produce, sin necesitar una impresora.
//
// Truco: se le pide al agente que imprima a una "térmica de red" que en realidad
// es un socket TCP local que solo guarda lo que recibe. Así se compara el flujo
// REAL del agente JS contra el REAL del agente Go, sin tocar el código de
// ninguno de los dos.
//
// Uso:  node capturar.js <urlDelAgente> <carpetaDeSalida> [puertoDelSumidero]

const net = require("net");
const fs = require("fs");
const path = require("path");

const [, , urlAgente, carpeta, puertoArg] = process.argv;
if (!urlAgente || !carpeta) {
  console.error("Uso: node capturar.js <urlDelAgente> <carpetaDeSalida> [puerto]");
  process.exit(2);
}
const puertoSumidero = Number(puertoArg) || 9199;

const payloads = JSON.parse(fs.readFileSync(path.join(__dirname, "payloads.json"), "utf8"));
const impresora = { conexion: "red", red: { host: "127.0.0.1", port: puertoSumidero } };

fs.mkdirSync(carpeta, { recursive: true });

// Sumidero: acumula lo que llegue de cada conexión y lo entrega al que espere.
let pendiente = null;
const servidor = net.createServer(sock => {
  const trozos = [];
  sock.on("data", d => trozos.push(d));
  sock.on("end", () => {
    if (pendiente) {
      const resolver = pendiente;
      pendiente = null;
      resolver(Buffer.concat(trozos));
    }
  });
});

function esperarBytes(ms = 8000) {
  return new Promise((resolve, reject) => {
    const t = setTimeout(() => { pendiente = null; reject(new Error("no llegaron bytes a tiempo")); }, ms);
    pendiente = buf => { clearTimeout(t); resolve(buf); };
  });
}

async function capturar(nombre, cuerpo) {
  const espera = esperarBytes();
  const r = await fetch(`${urlAgente}/print`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ...cuerpo, impresora }),
  });
  const json = await r.json().catch(() => ({}));
  if (!r.ok || !json.ok) throw new Error(`${nombre}: el agente respondió ${r.status} ${JSON.stringify(json)}`);

  const bytes = await espera;
  const destino = path.join(carpeta, `${nombre}.bin`);
  fs.writeFileSync(destino, bytes);
  console.log(`  ${nombre.padEnd(8)} ${String(bytes.length).padStart(6)} bytes  ->  ${destino}`);
}

(async () => {
  await new Promise(res => servidor.listen(puertoSumidero, "127.0.0.1", res));
  console.log(`Sumidero escuchando en 127.0.0.1:${puertoSumidero}; capturando de ${urlAgente}`);
  try {
    for (const [nombre, cuerpo] of Object.entries(payloads)) {
      await capturar(nombre, cuerpo);
    }
  } catch (e) {
    console.error("ERROR:", e.message);
    process.exitCode = 1;
  } finally {
    servidor.close();
  }
})();
