# Inlining

> **¿Quieres saber más?** Lee [`cmd/compile/internal/inline/inl.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/inline/inl.go) (busca las constantes del presupuesto al principio, `CanInline` y `hairyVisitor.doNode`), y mira cómo está escrito [`unicode/utf8.DecodeRune`](https://github.com/golang/go/blob/go1.27.0/src/unicode/utf8/utf8.go) para que quepa en el presupuesto.

En [Escape Analysis](05-escape-analysis.md) vimos una línea de la salida de `-m` que decía `inlining call to normalizeBefore`. El inlining consiste en que el compilador copia el cuerpo de una función dentro de la función que la llama, en lugar de emitir una llamada. Es la optimización que hace posibles muchas otras, y Go es bastante conservador con ella. En este módulo veremos las reglas que usa el compilador para decidir, y cogeremos una función caliente que es *justo* demasiado grande para conseguir que quepa.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Sabrás cuánto cuesta una llamada a función, y qué gana el inlining además de eliminarla
- Entenderás el **presupuesto** (budget) de inlining de Go (80) y el modelo de coste que cuenta nodos del IR
- Sabrás qué sigue impidiendo el inlining en Go 1.27
- Sabrás leer en `-gcflags=-m=2` los mensajes "can inline ... with cost N" y "cannot inline ... cost N exceeds budget 80"
- Aplicarás la **separación fast path / slow path** para conseguir que una función caliente se haga inline

## El Concepto: Lo Que Aporta el Inlining

Una llamada a función en Go no es cara en términos absolutos, unos pocos nanosegundos como mucho. Pero en un bucle ajustado que solo hace unos pocos nanosegundos de trabajo real por iteración, la llamada *es* el trabajo. Una llamada supone:

- mover los argumentos a registros (y los resultados de vuelta), y quizá volcar valores vivos al stack alrededor de la llamada
- las propias instrucciones `CALL`/`RET`, más un prólogo de función que comprueba si el stack necesita crecer
- un muro para el optimizador: el bucle de la función llamante no puede mantener valores en registros a través de la llamada, no puede eliminar comprobaciones de límites basándose en lo que comprobó la función llamada y no puede ver que una rama nunca se toma

![El inlining sustituye una llamada por el cuerpo de la función; solo ocurre si su coste cabe en el presupuesto de 80](images/06-inline.webp)

El inlining elimina todo eso. El cuerpo de la función llamada pasa a formar parte de la llamante, y todo se optimiza en conjunto. Como veremos en el [siguiente módulo](07-inlining-escape.md), eso incluye el escape analysis.

La contrapartida es el tamaño del código: una función que se hace inline en 50 sitios se copia 50 veces, y eso cuesta tamaño del binario, tiempo de compilación y caché de instrucciones (desde Go 1.27, los closures idénticos dentro de esas copias pueden al menos compartir un mismo cuerpo). Por eso el compilador solo hace inline de funciones pequeñas.

### El presupuesto

Estos son los parámetros, al principio de `inline/inl.go`:

```go
const (
	inlineMaxBudget       = 80
	inlineExtraAppendCost = 0
	// default is to inline if there's at most one call. -l=4 overrides this by using 1 instead.
	inlineExtraCallCost  = 57              // 57 was benchmarked to provided most benefit with no bad surprises; ...
	inlineParamCallCost  = 17              // calling a parameter only costs this much extra ...
	inlineExtraPanicCost = 1               // do not penalize inlining panics.
	inlineExtraThrowCost = inlineMaxBudget // with current (2018-05/1.11) code, inlining runtime.throw does not help.

	inlineBigFunctionNodes      = 5000                 // Functions with this many nodes are considered "big".
	inlineBigFunctionMaxCost    = 20                   // Max cost of inlinee when inlining into a "big" function.
	inlineClosureCalledOnceCost = 10 * inlineMaxBudget // if a closure is just called once, inline it.
)
```

El "coste" de una función lo calcula el `hairyVisitor`, que recorre el IR de la función (el árbol sintáctico del compilador) y cobra **1 por nodo**, con una lista de excepciones. Las importantes son:

- Una llamada a una función que **no se puede hacer inline** cuesta **57** extra. Con un presupuesto de 80, quedan unos 20 nodos para todo lo demás.
- Una llamada a una función que **sí** se puede hacer inline cuesta, en su lugar, el coste de inlining de esa función. Eso es el **mid-stack inlining**: `A` llama a `B`, que llama a `C`, y si `C` es barata, se integra en `B`, que todavía puede caber en `A`.
- `panic` cuesta 1. Las llamadas a los helpers de carga y almacenamiento de `encoding/binary` / `internal/byteorder` se tratan como baratas, porque se compilan a una sola instrucción.
- Dentro de una función llamante "grande" (más de 5000 nodos), solo se hace inline de funciones con coste ≤ 20.
- Los closures tienen el doble de presupuesto, y un closure que se llama una sola vez tiene 800.

### Qué impide el inlining en Go 1.27

Algunas construcciones hacen que una función no se pueda hacer inline sea cual sea su coste. En `hairyVisitor.doNode`, los bloqueos definitivos son:

```go
case ir.ORECOVER:
	v.reason = "call to recover"
	return true
...
case ir.OGO, ir.ODEFER, ir.OTAILCALL:
	v.reason = "unhandled op " + n.Op().String()
	return true
```

A esto se suman la directiva `//go:noinline` y algunos casos internos del runtime (funciones que llaman a `GetCallerPC`/`GetCallerSP`, algunos pragmas `//go:`). Cosas que *antes* impedían el inlining en versiones antiguas de Go y ya no lo hacen: bucles `for`, `select`, type switches, closures, `break`/`continue` con etiqueta. Las comprobamos todas con Go 1.27: se hacen inline sin problema siempre que quepan en el presupuesto. Muchos artículos antiguos están desactualizados en esto, así que, en caso de duda, pregúntale al compilador.

### Leyendo la respuesta del compilador

`-gcflags=-m` imprime `can inline F` e `inlining call to F`. `-m=2` añade el coste de todo, y el motivo en el caso de las funciones que no lo consiguieron:

```
./q.go:31:6: can inline small with cost 4 as: func(int) int { return x * 2 }
./q.go:3:6: can inline sel with cost 14 as: func(chan int) int { select statement }
./q.go:28:6: cannot inline def: unhandled op DEFER
./q.go:29:6: cannot inline rec: call to recover
```

Fíjate en que los "cannot inline" y los costes solo se imprimen con `-m=2`, no con `-m`.

### PGO

Desde Go 1.21, un perfil de CPU pasado con `-pgo` sube el presupuesto a **2000** para los puntos de llamada que están calientes en el perfil (`inlineHotMaxBudget`). Eso se trata en [Optimización Guiada por Perfiles](18-pgo.md). Sin un perfil, la regla de 80 es todo lo que hay.

## El Laboratorio

```
labs/06-inlining/
  before.go      implementación ingenua (referencia, no lo edites)
  after.go       tu copia: haz que esta sea rápida
  solution.go    solución de referencia (se compila con -tags solution)
  lab_test.go    test de corrección + benchmarks
```

Decodificamos un bloque de timestamps, tal y como los guarda una base de datos de series temporales: cada entrada es un *delta-of-delta* (el cambio en el intervalo entre muestras), codificado en zigzag y escrito como varint. Para métricas recogidas cada 10 segundos, el delta-of-delta casi siempre es diminuto, así que **casi todos los varints ocupan un solo byte**. `DecodeTimestampsBefore` llama a `uvarintBefore` una vez por timestamp, 10.000 veces por bloque.

### Paso 1: Mide la línea base

```bash
make bench LAB=06
```

### Paso 2: Encuentra la llamada

El perfil muestra `uvarintBefore` como una función independiente, lo que ya te dice que no se ha hecho inline:

```bash
cd labs
go test -run '^$' -bench 'Decode/impl=before' -cpuprofile cpu.prof ./06-inlining/
go tool pprof -top cpu.prof | grep -E 'flat%|uvarint|DecodeTimestamps'
```

```
      flat  flat%   sum%        cum   cum%
     670ms 65.05% 65.05%     1030ms   100%  labs/06-inlining.DecodeTimestampsBefore
     360ms 34.95%   100%      360ms 34.95%  labs/06-inlining.uvarintBefore
```

Ahora pregúntale al compilador por qué:

```bash
go build -gcflags=-m=2 ./06-inlining/ 2>&1 | grep -E 'inline.*(uvarint|DecodeTimestamps)' | cut -c1-110
```

```
06-inlining/before.go:16:6: cannot inline uvarintBefore: function too complex: cost 82 exceeds budget 80
06-inlining/before.go:42:6: cannot inline DecodeTimestampsBefore: function too complex: cost 115 exceeds budget 80
```

**82.** Dos nodos por encima. La función es un bucle con unas cuantas comprobaciones de validez (demasiado largo, desbordamiento, codificación no mínima), y todas son necesarias: no podemos borrarlas sin más.

### Paso 3: Arréglalo separando fast path / slow path

Este truco se usa por toda la librería estándar: mantener una función diminuta que resuelve el **caso común** y se hace inline, y mover todo lo demás a una función aparte a la que llama. De `sync.Once`:

```go
if !o.done.Load() {
	// Outlined slow-path to allow inlining of the fast-path.
	o.doSlow(f)
}
```

`sync.Mutex.Lock`, `sync.RWMutex.RLock` y `unicode/utf8.DecodeRune` hacen lo mismo. Aquí el caso común es "el primer byte es menor que `0x80`", y entonces el valor es ese byte.

Pero recuerda el modelo de coste: solo la llamada al slow path ya cuesta 57 más sus argumentos. El fast path tiene unos 20 nodos para gastar, así que su *forma exacta* importa. Mira `utf8.DecodeRune`:

```go
func DecodeRune(p []byte) (r rune, size int) {
	// Inlineable fast path for ASCII characters; see #48195.
	// This implementation is weird but effective at rendering the
	// function inlineable.
	for _, b := range p {
		if b < RuneSelf {
			return rune(b), 1
		}
		break
	}
	r, size = decodeRuneSlow(p)
	return
}
```

`go build -gcflags=-m=2 unicode/utf8` informa de `can inline DecodeRune with cost 79`. Le sobra un nodo.

Edita `labs/06-inlining/after.go`:

1. Renombra el bucle actual a `uvarintSlow` (con el mismo cuerpo).
2. Escribe una nueva `uvarintAfter(buf []byte) (x uint64, n int)` con la forma de `DecodeRune`: un bucle `range` que devuelve `uint64(b), 1` para un byte menor que `0x80` y en otro caso hace `break`, y después `x, n = uvarintSlow(buf)` y `return`.
3. Comprueba: `go build -gcflags=-m=2 ./06-inlining/ 2>&1 | grep uvarint`.

Deberías ver:

```
06-inlining/solution.go:22:6: cannot inline uvarintSlow: function too complex: cost 82 exceeds budget 80
06-inlining/solution.go:9:6: can inline uvarintAfter with cost 79 as: func([]byte) (uint64, int) { ...
06-inlining/solution.go:49:23: inlining call to uvarintAfter
```

¿Cómo de frágil es esto? Estos son los costes de variantes con el *mismo comportamiento* que medimos con Go 1.27:

| Forma del fast path | Coste |
|---|---|
| `range` + `break`, después `x, n = uvarintSlow(buf); return` | **79** ✓ |
| `if len(buf) > 0 && buf[0] < 0x80 {...}`, después `x, n = ...; return` | 81 ✗ |
| `range` + `break`, después `return uvarintSlow(buf)` | 85 ✗ |

El modelo de coste cuenta nodos del IR, no instrucciones, y algunas construcciones de Go se reescriben en más nodos que otras (`return f()` con dos resultados pasa por temporales adicionales). Este es el lado oscuro de un presupuesto fijo: si dependes de que una función se haga inline, **añade un test o una comprobación en CI** (el propio repositorio de Go tiene `TestIntendedInlining` en `cmd/compile/internal/test/inl_test.go` justo para eso).

### Paso 4: Compara

```bash
make bench LAB=06
```

Estos son los resultados con la solución de referencia (`make solution LAB=06`) en un AMD Ryzen AI MAX+ 395:

```
          │   before    │               after                │
          │   sec/op    │   sec/op     vs base               │
Decode-32   19.32µ ± 2%   10.19µ ± 2%  -47.26% (p=0.000 n=8)

          │   before   │             after             │
          │    B/op    │    B/op     vs base           │
Decode-32   0.000 ± 0%   0.000 ± 0%  ~ (p=1.000 n=8) ¹
¹ all samples are equal

          │   before   │             after             │
          │ allocs/op  │ allocs/op   vs base           │
Decode-32   0.000 ± 0%   0.000 ± 0%  ~ (p=1.000 n=8) ¹
¹ all samples are equal
```

## Entendiendo los Resultados

El decodificador es aproximadamente **2 veces más rápido**, y no hemos cambiado el algoritmo: el slow path es exactamente el mismo código que antes.

¿De verdad es el inlining, o es simplemente que el fast path hace menos trabajo que el bucle completo? Lo comprobamos añadiendo `//go:noinline` a `uvarintAfter` en la solución, sin tocar nada más. El resultado en la misma máquina: **alrededor de un -6%** en lugar de -47%. Casi toda la ganancia viene de que la llamada desaparece y de lo que puede hacer el compilador una vez que el fast path forma parte del bucle (mantener `t`, `delta` y el slice en registros, y eliminar la comprobación `n <= 0`, que en el fast path siempre es falsa).

Fíjate en que `DecodeTimestampsAfter` sigue sin poder hacerse inline (coste 137). No le hace falta: se llama una vez por bloque. **El inlining importa para la función llamada dentro del bucle caliente, no para la función que lo contiene.**

> **Cuándo no merece la pena:** si la función hace mucho trabajo por llamada (calcular el hash de un kilobyte, una syscall, una búsqueda en un map grande), el coste de la llamada es ruido. El inlining importa para funciones pequeñas que se llaman millones de veces, como accesores, decodificadores, comparadores y predicados.

## Puntos Clave

- El inlining elimina la llamada, y permite que el optimizador vea la función llamante y la llamada como una sola función
- Go hace inline de las funciones cuyo coste en IR es ≤ **80**. Una llamada que no se puede hacer inline consume **57** de ese presupuesto
- `recover`, `defer`, `go` y `//go:noinline` siguen impidiendo el inlining en Go 1.27. Los bucles, los closures y `select` no
- `-gcflags=-m=2` imprime el coste de cada función y por qué no se puede hacer inline
- **Separación fast path / slow path**: una función diminuta que se puede hacer inline para el caso común, que llama a una función aparte para el resto
- La forma exacta del código cambia el coste: verifícalo con `-m=2`, y protege las decisiones de inlining importantes con un test
- PGO puede subir el presupuesto en los puntos de llamada calientes (módulo 18)

## Para Ir Más Allá (opcional)

1. Añade `//go:noinline` a tu `uvarintAfter` y vuelve a ejecutar el benchmark. ¿Cuánto queda de la ganancia?
2. Cambia `testBlock` para que los intervalos sean irregulares (por ejemplo `t += int64(rand.IntN(100_000))`). Ahora la mayoría de los varints ocupan varios bytes. ¿Qué pasa con la mejora? ¿Por qué?
3. Compila con `-gcflags=-l` (desactiva el inlining) y compara `before` consigo mismo. ¿Qué llamadas a la librería estándar de este paquete dejan de hacerse inline? (`-m` te lo dirá.)
