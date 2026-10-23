# Interfaces, Boxing y Genéricos

> **¿Quieres saber más?** Lee [`runtime/iface.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/iface.go) (las funciones `convT*` y `staticuint64s`), `dataWord` en [`cmd/compile/internal/walk/convert.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/walk/convert.go), el desvirtualizador en [`cmd/compile/internal/devirtualize/devirtualize.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/devirtualize/devirtualize.go), y el [diseño de GC shape stenciling](https://github.com/golang/proposal/blob/master/design/generics-implementation-gcshape.md).

Las interfaces están por todas partes en Go: `io.Reader`, `error`, `any`, `fmt.Stringer`, cada punto de extensión de cada librería. También son el lugar donde se juntan los tres módulos anteriores. Meter un valor en una interfaz puede provocar una asignación (escape analysis), a una llamada a un método a través de una interfaz no se le puede hacer inlining (inlining), y como el compilador no ve la función llamada, todo lo que le pases escapa. En este módulo veremos qué es una interfaz en memoria, cuándo cuesta algo y qué hacer al respecto, incluido si los genéricos ayudan (spoiler: no de la forma que piensas).

## Objetivos de Aprendizaje

Al terminar este módulo:

- Conocerás el layout en memoria de los valores de interfaz (`iface`, `eface`, `itab`)
- Entenderás el **boxing**: cuándo convertir un valor a una interfaz provoca una asignación, y cuándo no
- Sabrás por qué el despacho dinámico bloquea el inlining y hace que los argumentos escapen
- Sabrás cuándo el compilador **desvirtualiza** una llamada a una interfaz
- Entenderás la implementación de los genéricos de Go (**GC shape stenciling** con diccionarios) y sus consecuencias en el rendimiento
- Eliminarás el boxing con un tipo concreto de "valor etiquetado", como hace `log/slog`

## El Concepto: Dos Palabras y una Tabla

Un valor de interfaz siempre ocupa dos palabras de máquina (`runtime/runtime2.go`):

```go
type iface struct {   // an interface with methods, like io.Reader
	tab  *itab
	data unsafe.Pointer
}

type eface struct {   // the empty interface, any
	_type *_type
	data  unsafe.Pointer
}
```

![Un valor de interfaz son dos palabras: un puntero al itab y un puntero a los datos. Guardar un valor que no es puntero lo encajona en el heap](images/08-iface.webp)

La primera palabra dice qué hay dentro. Para `any` es solo el descriptor de tipo. Para una interfaz no vacía es un **itab** (`internal/abi/iface.go`):

```go
type ITab struct {
	Inter *InterfaceType
	Type  *Type
	Hash  uint32     // copy of Type.Hash. Used for type switches.
	Fun   [1]uintptr // variable sized. fun[0]==0 means Type does not implement Inter.
}
```

`Fun` es la tabla de métodos: un puntero a código por cada método de la interfaz, para este tipo concreto. Una llamada `r.Read(p)` se compila como "carga `tab`, carga `tab.Fun[i]`, llámalo de forma indirecta pasando `data` como receptor". El itab de cada pareja (interfaz, tipo) se construye una sola vez (en tiempo de compilación cuando es posible, o en el primer uso, y después se guarda en una tabla hash), así que el despacho en sí es barato: dos cargas y una llamada indirecta.

### Boxing: ¿a dónde apunta `data`?

La segunda palabra es **siempre un puntero**. Si el valor ya es un puntero (o tiene forma de puntero: un map, un chan, una func, un struct con un único campo puntero), va directamente en `data`. Cualquier otra cosa hay que guardarla en algún sitio, y `data` apunta a ella. Eso es el boxing.

El compilador decide dónde vive la caja en `walk/convert.go` (`dataWord`), intentando evitar una asignación, en este orden:

1. Los valores de **tamaño cero** apuntan a una variable global, `zerobase`.
2. Los valores **bool y de un solo byte** apuntan dentro de `runtime.staticuint64s`, una tabla con los números 0–255.
3. Las **constantes y los valores cero** de literales compuestos apuntan a datos estáticos de solo lectura.
4. Las **variables globales de solo lectura** se usan directamente.
5. Si el valor de interfaz **no escapa** (y ocupa ≤ 1024 bytes), la caja es un **temporal en el stack**.

Solo si no se cumple ninguna de estas llama al runtime, eligiendo una función especializada según el tamaño y la forma (`convT16`, `convT32`, `convT64`, `convTstring`, `convTslice`, o las genéricas `convT`/`convTnoptr`). Este es `convT64`, de `runtime/iface.go`:

```go
func convT64(val uint64) (x unsafe.Pointer) {
	if val < uint64(len(staticuint64s)) {
		x = unsafe.Pointer(&staticuint64s[val])
	} else {
		x = mallocgc(8, uint64Type, false)
		*(*uint64)(x) = val
	}
	return
}
```

Es decir: hacer boxing de un `int` entre 0 y 255 es gratis incluso en tiempo de ejecución, y uno mayor es una asignación de 8 bytes. Hacer boxing de un `string` no vacío reserva 16 bytes para la cabecera del string (`convTstring`), no para sus bytes. Un `time.Duration` es un `int64`, así que también pasa por `convT64`. Los valores de 2, 4 u 8 bytes pasan por `convT16/32/64`, que solo tienen el atajo de números pequeños para valores menores que 256.

**La regla 5 es la clave**: el boxing solo provoca una asignación cuando el valor de interfaz escapa. Y los valores de interfaz escapan mucho, por el siguiente motivo.

### El despacho dinámico es un muro

Cuando el compilador ve `h.Handle(msg, fields)` con `h` de tipo interfaz, no sabe qué función se va a ejecutar. Así que:

- **No puede hacer inlining** de la llamada. El cuerpo del método es desconocido.
- El escape analysis **asume lo peor** para todos los argumentos: se filtran al heap. Cada puntero que pasas, y cada valor con boxing alcanzable desde él, escapa.

Ese segundo punto es lo que hace que las APIs llenas de interfaces provoquen asignaciones. Un valor metido en un `any` que solo se usa localmente tiene su caja en el stack. El mismo valor pasado a una interfaz `Handler` va al heap.

### Desvirtualización

Si el compilador *sí* puede demostrar cuál es el tipo concreto detrás de una interfaz, reescribe la llamada como una llamada directa (y después puede hacerle inlining). Esto es la desvirtualización estática, y Go 1.26 mejoró el análisis que encuentra el tipo concreto (`go126ImprovedConcreteTypeAnalysis` en `devirtualize.go`, que sigue a `true` y sin cambios en Go 1.27). Por ejemplo:

```go
func Count(vals []float64, min float64) (n int) {
	var f Filter = Threshold{min}
	for _, v := range vals {
		if f.Keep(v) {
			n++
		}
	}
	return n
}
```

```
./d.go:11:12: devirtualizing f.Keep to Threshold
./d.go:11:12: inlining call to Threshold.Keep
./d.go:9:26: Threshold{...} does not escape
```

Esto funciona cuando el tipo concreto es visible en la misma función, posiblemente después del inlining. No funciona cuando la interfaz viene de un parámetro de una función sin inlining, de un campo de un struct o de un slice de interfaces. Para esos casos existe la **desvirtualización guiada por perfiles**, que añade una comprobación "si es el tipo habitual, llámalo directamente" basada en un perfil (mira [Optimización Guiada por Perfiles](18-pgo.md)).

### Genéricos: GC shape stenciling

Los genéricos de Go no son plantillas de C++. El compilador no genera una copia de una función genérica por cada argumento de tipo. Genera una copia por cada **GC shape**: todos los tipos puntero comparten una forma, y el resto de tipos se agrupan por su tipo subyacente. Cada instanciación recibe además un argumento oculto, un **diccionario**, con la información de tipos, incluidos los punteros a los métodos de los argumentos de tipo.

La consecuencia: dentro de la función genérica, una llamada a un método de un parámetro de tipo es una llamada indirecta a través del diccionario, muy parecida a una llamada a una interfaz. El desvirtualizador renuncia a ella de forma explícita:

```go
// If typ is a shape type, then it was a type argument originally
// and we'd need an indirect call through the dictionary anyway.
// We're unable to devirtualize this call.
if typ.IsShape() {
	return
}
```

Vamos a medirlo. El laboratorio incluye una demostración que llama al mismo método diminuto 10.000 veces de cuatro formas:

```bash
cd labs
go run ./08-interfaces-boxing/cmd/dispatch
```

```
interface                       10.71 µs/op
generic, value type arg         10.61 µs/op
generic, pointer type arg       10.51 µs/op
concrete type                    4.10 µs/op
```

Las versiones genéricas no son más rápidas que la interfaz, tanto con un argumento de tipo valor como con uno puntero (las tres versiones indirectas varían un ±20% de una ejecución a otra; la concreta no). Solo el tipo concreto consigue el inlining de `Keep` (`-m` muestra `inlining call to Threshold.Keep` solo para `countConcrete`). Los genéricos son estupendos para contenedores con seguridad de tipos y para algoritmos sobre `==`/`<`/aritmética (esas operaciones se compilan directamente para la forma). **No** son una herramienta para librarse del coste del despacho de métodos. Los nuevos **métodos genéricos** de Go 1.27 (métodos con sus propios parámetros de tipo) se compilan igual: `func (s *Stats) Count[F Filter](vals []float64, f F)` se convierte en `(*Stats).Count[go.shape.struct { Min float64 }]` más un diccionario, y en un benchmark rápido fue tan rápido como la función genérica, ~4 veces más lento que un método concreto. (Y un método genérico no puede implementar un método de una interfaz, así que tampoco sustituye a las interfaces como punto de extensión).

## El Laboratorio

```
labs/08-interfaces-boxing/
  before.go         naive implementations (baseline, don't edit)
  after.go          your copy: make this one fast
  solution.go       reference solution (built with -tags solution)
  lab_test.go       correctness test + benchmarks
  cmd/dispatch/     the demo program above
```

Hay dos partes:

1. **`Log`**: un logger estructurado diminuto al estilo de `log/slog`. Los campos son `{Key string; Value any}`, y el logger los envía a una interfaz `Handler` intercambiable. Registramos 1.000 peticiones HTTP con 5 campos cada una.
2. **`Stats`**: número, suma y máximo de los valores que conserva una interfaz `Filter`, sobre 10.000 valores.

### Paso 1: Mide la línea base

```bash
make bench LAB=08
```

`Log` hace **5.000 asignaciones** por cada 1.000 peticiones. `Stats` no reserva nada, pero no pierdas de vista su tiempo.

### Paso 2: Encuentra las cajas

```bash
cd labs
go build -gcflags=-m ./08-interfaces-boxing/ 2>&1 | grep before.go | grep -E 'escape|leak'
```

Las líneas interesantes:

```
08-interfaces-boxing/before.go:45:42: fields does not escape
08-interfaces-boxing/before.go:71:41: leaking param: fields
08-interfaces-boxing/before.go:78:9: ... argument escapes to heap
08-interfaces-boxing/before.go:79:10: v escapes to heap
08-interfaces-boxing/before.go:80:10: v escapes to heap
08-interfaces-boxing/before.go:81:7: v escapes to heap
08-interfaces-boxing/before.go:82:7: v escapes to heap
08-interfaces-boxing/before.go:83:12: v escapes to heap
08-interfaces-boxing/before.go:102:34: leaking param: f
```

- Línea 45: `(*TextHandler).Handle` en sí no se queda con `fields`. Si el compilador pudiera verlo, nada escaparía.
- Línea 71: pero `Info` llama a `l.h.Handle(msg, fields)` a través de la interfaz, así que `fields` se filtra.
- Líneas 78–83 (con inlining dentro de `LogRequestsBefore`): el slice variádico `[]Field` escapa, y también cada valor con boxing `v`.

Contemos las asignaciones por petición: el slice de 5 `Field`s (5 × 32 = 160 bytes), `"GET"` y la ruta (`convTstring`, 16 bytes cada uno), `Bytes` (≥ 256, `convT64`, 8 bytes), `Latency` (`convT64`, 8 bytes). Son 5 asignaciones y 208 bytes. ¿Y `Status`? Vale `200`, menos de 256, así que apunta dentro de `staticuint64s` y no cuesta nada. Un `404` costaría una asignación.

Para `Stats`, la línea 102 dice `leaking param: f`, y el perfil mostraría el método `Threshold.Keep` como una función aparte llamada 10.000 veces (a través de un wrapper autogenerado `(*Threshold).Keep`, porque los métodos del itab reciben el puntero `data` de la caja).

### Paso 3: Arréglalo

Edita `labs/08-interfaces-boxing/after.go`.

**Parte 1, paso 1: elimina el boxing.** Sustituye `Value any` por un tipo concreto pequeño que pueda contener todos los tipos de valor que registramos sin usar una interfaz:

```go
type Value struct {
	kind Kind   // KindString, KindInt, KindDuration
	num  uint64 // ints and durations
	str  string // strings
}
```

Actualiza `StringAfter`, `IntAfter`, `DurationAfter` y el `switch` de `TextHandlerAfter.Handle` (haz el switch sobre `kind` en lugar de sobre el tipo dinámico). Esto es exactamente lo que hace `log/slog.Value` (de forma más compacta: guarda el tipo, o un puntero a los bytes del string, en un campo `any` cuyo contenido nunca necesita boxing).

Ejecuta el benchmark: **5 asignaciones por petición pasan a ser 1**, pero los bytes *suben* (de 208 a 240 por petición), porque la asignación que queda es el slice `[]FieldAfter` y un `FieldAfter` ahora ocupa 48 bytes. Medimos ese paso intermedio: de 5.000 a 1.000 allocs/op, y de 203 KiB a 234 KiB.

**Parte 1, paso 2: evita que el slice escape.** El slice escapa porque se pasa a un método de una interfaz. Pasa un **valor** en su lugar: un `Record` con los campos copiados en un array de tamaño fijo:

```go
type Record struct {
	Msg   string
	n     int
	front [6]FieldAfter
	back  []FieldAfter // only for the rare entry with more than 6 fields
}

type HandlerAfter interface {
	Handle(r Record)
}
```

`Info` copia `fields` en `r.front` y llama a `l.h.Handle(r)`. Un struct pasado por valor se copia en los argumentos de la llamada, así que solo su *contenido* (los strings) puede filtrarse, nunca el slice variádico en sí. Así funciona también `log/slog.Record`: su array `front [nAttrsInline]Attr` (5 atributos) existe precisamente para evitar esta asignación.

**Parte 2: `Stats`.** Prueba primero a hacer `StatsAfter` genérica:

```go
func StatsAfter[F FilterAfter](vals []float64, f F) (n int, sum, hi float64)
```

Mide. Después prueba con el tipo concreto: `func StatsAfter(vals []float64, t ThresholdAfter) ...`. El benchmark llama a `StatsAfter(vals, ThresholdAfter{Min: minFlag})`, que compila con las tres firmas.

### Paso 4: Compara

```bash
make bench LAB=08
```

Estos son los resultados con la solución de referencia (`make solution LAB=08`) en un AMD Ryzen AI MAX+ 395:

```
         │    before    │               after                │
         │    sec/op    │   sec/op     vs base               │
Log-32     108.25µ ± 3%   62.44µ ± 2%  -42.32% (p=0.000 n=8)
Stats-32    26.88µ ± 0%   10.30µ ± 0%  -61.70% (p=0.000 n=8)
geomean     53.94µ        25.35µ       -53.00%

         │      before      │                after                │
         │       B/op       │    B/op     vs base                 │
Log-32     208029.00 ± 0%     14.50 ± 3%  -99.99% (p=0.000 n=8)
Stats-32       0.000 ± 0%     0.000 ± 0%        ~ (p=1.000 n=8) ¹
geomean                   ²               -99.17%               ²
¹ all samples are equal
² summaries must be >0 to compute geomean

         │    before     │                  after                  │
         │   allocs/op   │  allocs/op   vs base                    │
Log-32     5.000k ± 0%     0.000k ± 0%  -100.00% (p=0.000 n=8)
Stats-32    0.000 ± 0%      0.000 ± 0%         ~ (p=1.000 n=8) ¹
geomean                ²                ?                      ² ³
¹ all samples are equal
² summaries must be >0 to compute geomean
³ ratios must be >0 to compute geomean
```

## Entendiendo los Resultados

**Log**: las 5.000 asignaciones y 203 KiB por cada 1.000 peticiones bajan a cero (los ~15 B/op que quedan son el buffer de salida creciendo de vez en cuando), y el tiempo baja un ~40%. Seguimos pasando por la interfaz `Handler` en cada entrada. Simplemente hemos dejado de darle cosas que necesitan el heap. Fíjate en que ahora copiamos un `Record` de ~330 bytes en cada llamada, y aun así es mucho más rápido que reservar memoria. Copiar memoria que está caliente en la caché es barato; reservarla cuesta un `mallocgc` y, después, trabajo del GC.

**Stats**: 2,6x más rápido con el tipo concreto. La comparación `v >= t.Min` se integra en el bucle con inlining, lo que supone 10.000 llamadas indirectas menos. Con la versión genérica deberías ver poca o ninguna mejora, igual que en la demostración `cmd/dispatch`.

¿Cuándo deberías mantener la interfaz? ¡Casi siempre! Una llamada a una interfaz cuesta un par de nanosegundos. Solo importa cuando está en un bucle caliente con poco trabajo por llamada, o cuando provoca asignaciones por culpa de los escapes. Cuando sí importa, las opciones son:

- Usar el **tipo concreto** en la ruta caliente (o un type switch que elija una ruta rápida concreta para los tipos más habituales)
- Sacar la frontera de la interfaz **fuera del bucle**: una llamada por lote en lugar de una por elemento (`io.Reader` lee un `[]byte`, no un byte cada vez)
- Pasar **valores** en lugar de punteros/slices a través de la interfaz, para que nada escape
- Usar un perfil y dejar que **PGO** desvirtualice los puntos de llamada calientes ([módulo 18](18-pgo.md))

## Puntos Clave

- Un valor de interfaz son dos palabras: un puntero al tipo/itab y un puntero a los datos
- Los valores que no son punteros pasan por **boxing**. Los valores de tamaño cero, los bools, los bytes y los enteros 0–255 usan almacenamiento estático, las constantes usan datos de solo lectura, y las cajas que no escapan van al stack. Todo lo demás es una asignación en el heap con `convT*`
- Las llamadas a métodos de una interfaz no admiten inlining, y **todos sus argumentos escapan**
- El compilador **desvirtualiza** cuando puede ver el tipo concreto (mejorado en Go 1.26). PGO cubre más casos
- Los genéricos de Go usan **GC shape stenciling + diccionarios**: las llamadas a métodos de parámetros de tipo siguen siendo indirectas, y para eso los genéricos no son más rápidos que las interfaces
- Un valor etiquetado concreto (como `slog.Value`) y pasar registros por valor (como `slog.Record`) evitan las asignaciones por boxing al cruzar una interfaz

## Para Ir Más Allá (opcional)

1. Cambia `Status` en `testRequests` a `1000 + i%10`. ¿Cuántas asignaciones por petición hace ahora la versión "before"? ¿Por qué?
2. En la solución, cambia `Handle(r Record)` por `Handle(r *Record)`. ¿Qué dice `-m` sobre el `Record` en `Info`?
3. Escribe un `StatsAfter` que reciba un `[]FilterAfter` (varios filtros, y todos deben conservar el valor). Después prueba a cambiar la interfaz a `KeepAll(vals []float64, keep []bool)`, una llamada por lote. ¿Cómo se compara?
4. Ejecuta `go build -gcflags=-m=2` sobre `cmd/dispatch` y busca `devirtualizing`. Después quita el `//go:noinline` de `countIface`. ¿Qué cambia, y por qué?
