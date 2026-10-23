# Inlining × Escape Analysis

> **¿Quieres saber más?** Mira cómo están divididos [`crypto/internal/fips140/aes.New`](https://github.com/golang/go/blob/go1.27.0/src/crypto/internal/fips140/aes/aes.go) y los métodos `Bytes` de [`crypto/internal/fips140/nistec`](https://github.com/golang/go/blob/go1.27.0/src/crypto/internal/fips140/nistec/p384.go), y lee el orden de los pases en [`cmd/compile/internal/gc/main.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/gc/main.go): el inlining se ejecuta **antes** que el escape analysis.

Cada uno de los dos módulos anteriores mostró un pase del compilador por separado. Este trata de cómo trabajan juntos, y es uno de los trucos más útiles en el Go sensible al rendimiento. En resumen: **el escape analysis examina una función después del inlining**. Cuando se hace inlining de un constructor, la asignación que contiene se traslada al llamador, y puede que el llamador consiga mantenerla en el stack. Aprovecharemos esto para que una API del tipo "`New` devuelve un puntero" no haga ninguna asignación.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Entenderás por qué importa el orden "primero inlining, después escape analysis"
- Sabrás por qué `func NewX() *X` normalmente asigna, y cuándo no lo hace
- Aplicarás el patrón **outlined constructor** («constructor outlined»): un `New` diminuto e inlinable que hace la asignación, más un `init` separado que hace el trabajo
- Leerás salida de `-m` en la que la misma asignación "escapa" en un sitio y "no escapa" en otro
- Encontrarás este patrón en la librería estándar

## El Concepto: Las Asignaciones Se Mueven Con el Inlining

La parte intermedia del compilador ejecuta, en orden: desvirtualización e **inlining** (intercalados), después **escape analysis** y después el backend. Para cuando el escape analysis examina una función, cada llamada a la que se le hizo inlining ya se ha sustituido por el cuerpo de la función llamada.

Considera un constructor:

```go
func NewDecoder(block []byte) (*Decoder, error) {
	// ... validate the header, check a CRC ...
	return &Decoder{src: block[headerSize:], left: count, t: first}, nil
}
```

Analizado por sí solo, `&Decoder{...}` fluye hacia el resultado `~r0`, así que **escapa**: la función retorna y su frame desaparece. Si no se hace inlining de `NewDecoder`, no hay más que hablar. Cada llamada asigna, haga lo que haga el llamador con el puntero.

Pero si **sí** se hace inlining de `NewDecoder` en este llamador:

```go
for _, block := range blocks {
	d, err := NewDecoder(block)
	...
	for { t, ok := d.Next(); ... }
}
```

...entonces `&Decoder{...}` pasa a ser una expresión dentro del bucle del llamador. Su "resultado" es simplemente la variable local `d`. Si `d` solo se usa en llamadas a métodos cuyos tags de parámetro dicen "`d` does not escape" (módulo 05), el `Decoder` puede vivir en el stack del llamador. Cero asignaciones, con exactamente la misma API.

Así que, para un constructor que devuelve un puntero, hay dos requisitos:

1. El constructor tiene que ser **inlinable** (coste ≤ 80, mira [Inlining](06-inlining.md)).
2. El **llamador** no debe dejar que el puntero escape (ni guardarlo en el heap, ni devolverlo, ni pasarlo a un método de una interfaz).

El segundo depende de quienes usen tu API. El primero depende de ti.

### El patrón outlined constructor

Los constructores reales validan, calculan y llaman a otras funciones, así que rara vez caben en 80. La solución es dividirlos, exactamente como la división entre ruta rápida y ruta lenta del módulo 06, pero esta vez la "ruta rápida" es la asignación:

```go
// Tiny and inlinable: the allocation happens in the caller.
func NewDecoder(block []byte) (*Decoder, error) {
	d := &Decoder{}
	if err := d.init(block); err != nil {
		return nil, err
	}
	return d, nil
}

// All the work. It only writes through d, so "d does not escape".
func (d *Decoder) init(block []byte) error { ... }
```

![Cuando un constructor pequeño se hace inline, su asignación se mueve al frame del llamador y puede quedarse en el stack](images/07-outlined.webp)

A `init` no se le hace inlining (es demasiado grande), pero no hace falta: lo que importa es que su tag de parámetro dice `d does not escape`, así que pasarle un puntero a un objeto del stack no es ningún problema.

No es un truco inventado. La librería estándar de Go lo hace, y lo dice en los comentarios:

```go
// crypto/internal/fips140/aes/aes.go
func New(key []byte) (*Block, error) {
	// This call is outline to let the allocation happen on the parent stack.
	return newOutlined(&Block{}, key)
}

// newOutlined is marked go:noinline to avoid it inlining into New, and making New
// too complex to inline itself.
//
//go:noinline
func newOutlined(b *Block, key []byte) (*Block, error) {
```

```go
// crypto/internal/fips140/nistec/p384.go
func (p *P384Point) Bytes() []byte {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var out [1 + 2*p384ElementLength]byte
	return p.bytes(&out)
}
```

Y `crypto/internal/fips140/sha256.New` es simplemente `d := new(Digest); d.Reset(); return d`. El segundo ejemplo también funciona con slices devueltos: el array de tamaño fijo se reserva en el llamador (después del inlining), y la función outlined solo lo rellena.

Fíjate en el `//go:noinline` de `newOutlined`. Sin él, el compilador podría hacer inlining de `newOutlined` dentro de `New` (si fuera lo bastante pequeña), y `New` pasaría a ser demasiado cara para que se le hiciera inlining en ningún sitio. El inlining se decide de abajo arriba, así que un helper al que se le hace inlining dentro de tu wrapper suma su coste al del wrapper.

## El Laboratorio

```
labs/07-inlining-escape/
  before.go      naive implementation (baseline, don't edit)
  after.go       your copy: make this one fast
  solution.go    reference solution (built with -tags solution)
  lab_test.go    correctness test + benchmarks
```

Volvemos a los bloques de timestamps del módulo 06, ahora con una cabecera en condiciones: bytes mágicos, versión, número de elementos, el primer timestamp y un CRC-32 de los datos. `NewDecoderBefore(block)` lo valida todo y devuelve un `*DecoderBefore`, que tiene un iterador `Next()`. `MaxGapBefore` recorre 1.000 bloques pequeños (16 timestamps cada uno) para encontrar el mayor hueco entre dos muestras, así que crea 1.000 decoders.

### Paso 1: Mide la línea base

```bash
make bench LAB=07
```

Verás **1.000 allocs/op**, una por bloque, y 78 KiB/op: el `Decoder` ocupa 72 bytes, redondeados hacia arriba a la clase de tamaño de 80 bytes.

### Paso 2: Pregúntale al compilador

```bash
cd labs
go build -gcflags=-m=2 ./07-inlining-escape/ 2>&1 | grep 'inline NewDecoderBefore' | cut -c1-110
go build -gcflags=-m ./07-inlining-escape/ 2>&1 | grep 'before.go' | grep -E ': .*(escape|leak)'
```

```
07-inlining-escape/before.go:44:6: cannot inline NewDecoderBefore: function too complex: cost 138 exceeds budg
07-inlining-escape/before.go:44:23: leaking param: block
07-inlining-escape/before.go:48:17: string(block[:2]) does not escape
07-inlining-escape/before.go:59:9: &DecoderBefore{...} escapes to heap
07-inlining-escape/before.go:64:7: d does not escape
07-inlining-escape/before.go:85:7: leaking param: d to result ~r0 level=1
07-inlining-escape/before.go:89:19: leaking param content: blocks
```

El constructor cuesta 138, así que no se le hace inlining, y `&DecoderBefore{...}` escapa a través de su valor de retorno. Pero fíjate en `MaxGapBefore`: el decoder solo se usa localmente, y `(*DecoderBefore).Next` indica `d does not escape`. Si la asignación estuviera en `MaxGapBefore`, podría quedarse en el stack.

### Paso 3: Arréglalo

Edita `labs/07-inlining-escape/after.go`:

1. Mueve el cuerpo de `NewDecoderAfter` a un método `func (d *DecoderAfter) init(block []byte) error`. Sustituye `return nil, ErrX` por `return ErrX`, y el `return &DecoderAfter{...}, nil` final por `*d = DecoderAfter{...}` y `return nil`.
2. Convierte `NewDecoderAfter` en el wrapper diminuto que vimos arriba.
3. No toques `MaxGapAfter`. La gracia está en que los llamadores no cambian.

Compruébalo:

```bash
go build -gcflags=-m=2 ./07-inlining-escape/ 2>&1 | grep -E 'inline (NewDecoderAfter|\(\*DecoderAfter\).init)' | cut -c1-110
go build -gcflags=-m ./07-inlining-escape/ 2>&1 | grep -E 'DecoderAfter\{\}'
```

Con la solución de referencia:

```
07-inlining-escape/solution.go:32:6: cannot inline (*DecoderAfter).init: function too complex: cost 136 exceed
07-inlining-escape/solution.go:22:6: can inline NewDecoderAfter with cost 80 as: func([]byte) (*DecoderAfter,
07-inlining-escape/solution.go:23:7: &DecoderAfter{} escapes to heap
07-inlining-escape/solution.go:80:28: &DecoderAfter{} does not escape
```

El mismo `&DecoderAfter{}` aparece **dos veces** con veredictos opuestos, y los dos son correctos:

- La línea 23 es la función `NewDecoderAfter` independiente. Sigue existiendo (alguien podría llamarla a través de un valor de función, o desde un paquete donde no se le haga inlining), y en esa copia el decoder sí escapa.
- La línea 80 es la copia a la que se le ha hecho inlining dentro de `MaxGapAfter`. Ahí no escapa, así que es una asignación en el stack.

Fíjate también en el coste: **80**, justo en el límite. Escribirlo como `d := new(DecoderAfter); err := d.init(block); return d, err` cuesta 72, pero devuelve un decoder no nil en caso de error. Elige la semántica que quieras y comprueba el coste.

### Paso 4: Compara

```bash
make bench LAB=07
```

Estos son los resultados con la solución de referencia (`make solution LAB=07`) en un AMD Ryzen AI MAX+ 395:

```
          │   before    │               after                │
          │   sec/op    │   sec/op     vs base               │
MaxGap-32   65.09µ ± 1%   51.29µ ± 3%  -21.20% (p=0.000 n=8)

          │    before    │                after                │
          │     B/op     │    B/op      vs base                │
MaxGap-32   78.12Ki ± 0%   0.00Ki ± 0%  -100.00% (p=0.000 n=8)

          │   before    │                after                │
          │  allocs/op  │  allocs/op   vs base                │
MaxGap-32   1.000k ± 0%   0.000k ± 0%  -100.00% (p=0.000 n=8)
```

## Entendiendo los Resultados

Las 1.000 asignaciones han desaparecido, y el tiempo baja aproximadamente una quinta parte, aunque en cada bloque se siga parseando la cabecera y calculando el CRC. En una ejecución más tranquila (fijada a un solo núcleo con `taskset`) medimos 94 µs → 49 µs. El número de "before" varía más de una ejecución a otra porque incluye trabajo de asignación y de GC, y eso depende de qué más esté haciendo la máquina.

La API no ha cambiado: `NewDecoderAfter` sigue devolviendo `(*DecoderAfter, error)`. Los llamadores que conservan el decoder (lo guardan en un struct, lo devuelven) siguen teniendo una asignación en el heap, lo cual es correcto. Los llamadores que lo usan localmente, que es el caso habitual para iteradores, readers, hashers y builders, lo obtienen gratis.

### Cuándo no funciona

- **El constructor devuelve una interfaz.** `crypto/sha256.New()` devuelve un `hash.Hash`. En un uso puramente local, `h := sha256.New(); h.Write(p); h.Sum(buf[:0])`, el compilador sigue viendo el tipo concreto después del inlining, y el desvirtualizador (mejorado en Go 1.26, sin cambios en 1.27) convierte las llamadas en directas: `-m` dice `devirtualizing h.Write to *sha256.Digest` y `new(sha256.Digest) does not escape`, 0 asignaciones. Pero pasa `h` a cualquier función no inlineada que reciba un `hash.Hash` y las llamadas vuelven a ser dinámicas, el escape analysis no puede seguirlas (mira el [siguiente módulo](08-interfaces-boxing.md)), y el `Digest` se va al heap. El `fips140/sha256.New()` interno devuelve `*Digest` y no depende de eso.
- **El llamador lo deja escapar**, por ejemplo guardando el decoder en un struct de larga duración o enviándolo por un channel.
- **El valor es demasiado grande** para el stack (64 KiB para `&T{}`, mira el módulo 05).
- **El wrapper se pasa del presupuesto.** Es fácil llevar un wrapper "diminuto" por encima de 80 añadiendo una comprobación más. Protégelo con un test que use `testing.AllocsPerRun`:

```go
func TestNewDecoderDoesNotAllocate(t *testing.T) {
	block := makeBlock(...)
	if n := testing.AllocsPerRun(100, func() { MaxGapAfter([][]byte{block}) }); n != 0 {
		t.Fatalf("got %v allocs, want 0", n)
	}
}
```

## Puntos Clave

- El inlining se ejecuta **antes** que el escape analysis, así que las decisiones de escape se toman sobre el código ya con inlining
- Un constructor sin inlining que devuelve un puntero **siempre** asigna. Uno con inlining asigna en el llamador, donde puede quedarse en el stack
- **Outlined constructor**: mantén `New` diminuto (asigna + llama a `init`) y haz el trabajo en `init(d *T)`, que no deja escapar a `d`
- La librería estándar hace esto en `crypto/internal/fips140` (`aes.New`/`newOutlined`, `nistec` `Bytes`/`bytes`, `sha256.New`)
- La misma asignación puede aparecer como que escapa en la función y como que no escapa en un punto de llamada con inlining. Las dos cosas son ciertas
- Devolver interfaces lo echa a perder en cuanto el compilador no puede desvirtualizar las llamadas. Comprueba el número de asignaciones con un test si dependes de ello

## Para Ir Más Allá (opcional)

1. Añade una validación más en tu wrapper `NewDecoderAfter` (por ejemplo, `if len(block) == 0 { return nil, ErrHeader }`) en lugar de en `init`. ¿Se le sigue haciendo inlining? ¿Qué dice el benchmark?
2. Haz que `MaxGapAfter` guarde todos los decoders en un slice (`var all []*DecoderAfter; all = append(all, d)`). ¿Qué dice ahora `-m` sobre `&DecoderAfter{}`?
3. Escribe el mismo benchmark para `crypto/sha256`: compara `sha256.New()` + `Write` + `Sum(buf[:0])` usado localmente, lo mismo pero con el `Write` hecho en un helper `//go:noinline` que recibe un `hash.Hash`, y `sha256.Sum256(data)`. Cuenta las asignaciones y explica la diferencia (busca `devirtualizing` en `-m`).
