package escpos

import (
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
)

// tablaCodigo asocia el nombre de juego de caracteres que manda la app (los
// mismos identificadores que usaba node-thermal-printer) con dos cosas:
// el número de página de códigos de la impresora (ESC t n) y el codificador
// que traduce el UTF-8 de la app a los bytes de esa página.
type paginaCodigos struct {
	n   byte
	enc encoding.Encoding
}

var tablaCodigo = map[string]paginaCodigos{
	"PC437_USA":             {0, charmap.CodePage437},
	"PC850_MULTILINGUAL":    {2, charmap.CodePage850},
	"PC860_PORTUGUESE":      {3, charmap.CodePage860},
	"PC863_CANADIAN_FRENCH": {4, charmap.CodePage863},
	"PC865_NORDIC":          {5, charmap.CodePage865},
	"WPC1252":               {16, charmap.Windows1252},
	"PC866_CYRILLIC2":       {17, charmap.CodePage866},
	"PC852_LATIN2":          {18, charmap.CodePage852},
	"SLOVENIA":              {18, charmap.CodePage852},
	"PC858_EURO":            {19, charmap.CodePage858},
	"PC855_CYRILLIC":        {34, charmap.CodePage855},
	"PC862_HEBREW":          {36, charmap.CodePage862},
	"ISO8859_2_LATIN2":      {39, charmap.ISO8859_2},
	"ISO8859_15_LATIN9":     {40, charmap.ISO8859_15},
	"WPC1250_LATIN2":        {45, charmap.Windows1250},
	"WPC1251_CYRILLIC":      {46, charmap.Windows1251},
	"WPC1253_GREEK":         {47, charmap.Windows1253},
	"WPC1254_TURKISH":       {48, charmap.Windows1254},
	"WPC1255_HEBREW":        {49, charmap.Windows1255},
	"WPC1256_ARABIC":        {50, charmap.Windows1256},
	"WPC1257_BALTIC_RIM":    {51, charmap.Windows1257},
	"WPC1258_VIETNAMESE":    {52, charmap.Windows1258},
}

// resolverCodigo devuelve la página de códigos pedida, cayendo a PC858_EURO
// (la que trae por defecto el POS, cubre acentos y ñ del español).
func resolverCodigo(nombre string) paginaCodigos {
	if p, ok := tablaCodigo[nombre]; ok {
		return p
	}
	return tablaCodigo["PC858_EURO"]
}
