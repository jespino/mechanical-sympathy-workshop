# Optimización Guiada por Perfiles

> **¿Quieres saber más?** Lee la [guía oficial de PGO](https://go.dev/doc/pgo), la lógica de call sites calientes en [`cmd/compile/internal/inline/inl.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/inline/inl.go) (busca `inlineHotMaxBudget` y `inlineCostOK`), y la desvirtualización con PGO en [`cmd/compile/internal/devirtualize/pgo.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/devirtualize/pgo.go).

El compilador toma sus decisiones sin saber qué código es caliente. Tiene que hacer un poco de inlining en todas partes, y no puede saber qué tipo concreto hay detrás de una interfaz. En todos los módulos hasta ahora, has sido *tú* quien ha usado un profile para arreglar el código. Con la optimización guiada por perfiles (PGO), disponible desde Go 1.21, le das el profile al compilador, y este lo usa para tomar mejores decisiones. Sin cambiar el código, solo con un build distinto.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Sabrás qué hace el compilador de Go con un profile en Go 1.27: inlining de call sites calientes, desvirtualización y alineación de loops calientes
- Conocerás el flag `-pgo` (`auto`, `off`, un fichero) y dónde tiene que estar `default.pgo`
- Recogerás un profile de CPU representativo y harás un build con él
- Leerás las decisiones de PGO del compilador con `-gcflags=-m` y `-d=pgodebug=1`
- Sabrás explicar por qué PGO puede eliminar *asignaciones* y no solo el coste de las llamadas
- Conocerás los límites y las trampas de PGO

## El Concepto: Darle el Profile al Compilador

### Inlining de call sites calientes

![PGO: compila, ejecuta con carga real, captura un perfil de CPU y recompila con él para que el compilador optimice las rutas calientes](images/18-pgo-loop.webp)

Viste en [Inlining](06-inlining.md) que el inliner asigna un coste a cada función y solo la hace inline si ese coste es como mucho `inlineMaxBudget = 80`. Con un profile, `cmd/compile/internal/inline/inl.go` añade un segundo presupuesto (budget), mucho más grande:

```go
// Threshold in CDF percentage for hot callsite inlining,
// that is, for a threshold of X the hottest callsites that
// make up the top X% of total edge weight will be
// considered hot for inlining candidates.
inlineCDFHotCallSiteThresholdPercent = float64(99)

// Budget increased due to hotness.
inlineHotMaxBudget int32 = 2000
```

El compilador ordena por peso las aristas de llamada del profile y marca como **calientes** las de más peso, las que suman el 99% del peso total. Después, `inlineCostOK` decide cada llamada:

```go
if metric <= maxCost {
	// Simple case. Function is already cheap enough.
	return true, 0, metric, hot
}

// We'll also allow inlining of hot functions below inlineHotMaxBudget,
// but only in small functions.
if !hot {
	// Cold
	return false, maxCost, metric, false
}
...
if bigCaller { ... return false, ... }   // caller has > 5000 IR nodes
if metric > inlineHotMaxBudget {
	return false, inlineHotMaxBudget, metric, false
}
```

Así que una función con coste 101 nunca se hace inline en un build normal. En un **call site caliente** dentro de un llamador no demasiado grande, puede hacerse inline hasta un coste de 2000. El resto del programa mantiene el budget normal, así que el binario apenas crece.

### Desvirtualización

Una llamada a un método de interfaz es una llamada indirecta a través del itab, así que el compilador no puede hacerla inline (mira [Interfaces y Boxing](08-interfaces-boxing.md)). El profile registra a qué método concreto llegó realmente cada call site. `devirtualize/pgo.go` usa esa información para reescribir el destino más caliente como una llamada directa protegida:

```go
// to:
//
//	func foo(i Iface) {
//		if c, ok := i.(Concrete); ok {
//			c.Foo()
//		} else {
//			i.Foo()
//		}
//	}
//
// The primary benefit of this transformation is enabling inlining of the
// direct call.
```

La llamada directa `c.Foo()` ya se puede hacer inline (con el budget caliente). La rama alternativa mantiene el programa correcto cuando aparece otro tipo. Lo mismo se hace con las llamadas a través de **valores de función** (`if FuncPC(fn) == FuncPC(Concrete) { Concrete() }`).

### Y un poco de layout

Cuando una función es caliente, el backend SSA también marca las cabeceras de sus loops, y en amd64 `ssagen` las alinea a una cache line de 64 bytes si quedan 31 bytes o menos en la línea actual (`hotAlign = 64`, `hotRequire = 31` en `ssagen/ssa.go`, controlado por `-d=alignhot`). Es un efecto pequeño, y solo se aplica en amd64 y 386.

### Cómo encuentra el profile el comando go

El flag de build `-pgo` (`go help build`):

- `-pgo=auto` (el valor por defecto): para cada **paquete main**, usa `default.pgo` del directorio de ese paquete, y lo aplica al paquete main y a todas sus dependencias
- `-pgo=off`: sin PGO
- `-pgo=path/to/file.pprof`: usa este profile para todos los paquetes del build

Dos consecuencias: un `default.pgo` en el directorio de una librería se ignora, y `go test` sobre un paquete que no es main nunca lo detecta automáticamente. Para los benchmarks pasas `-pgo=file` explícitamente. El profile usado queda registrado en el binario: `go version -m ./binary` muestra un ajuste de build `-pgo=`.

## El Laboratorio

Este laboratorio es distinto: **no hay `after.go`**. El código no cambia, solo cambia el build, así que `make bench LAB=18` no aplica. Comparas dos builds con `go test` normal y `benchstat old.txt new.txt`.

> **Atajo:** `make pgo` (desde la raíz del repositorio) ejecuta todo el ciclo de abajo de una vez: benchmark con `-pgo=off`, captura de un perfil de CPU, recompilación con él y la comparación con `benchstat`. Pero hazlo a mano la primera vez: lo importante de este módulo es ver cada paso.

```
labs/18-pgo/
  ingest.go         an access-log ingester (don't edit)
  lab_test.go       correctness test + BenchmarkIngest
  cmd/ingest/       the same workload as a main program, for default.pgo
```

`(*Aggregator).Ingest` parsea líneas como `17,200,1534\n` con `parseRecord`, que devuelve un `*Record`, y pasa cada registro a una interfaz `Observer`. En producción el observer es siempre un `*Histogram`, pero el compilador no puede saberlo. Ejecuta los diagnósticos del inliner para ver el punto de partida:

```bash
cd labs
go build -gcflags='labs/18-pgo=-m=2' ./18-pgo/ 2>&1 | grep -E 'parseRecord|Observe|Ingest|Record\{\}'
```

```
18-pgo/ingest.go:28:6: cannot inline parseRecord: function too complex: cost 101 exceeds budget 80
18-pgo/ingest.go:70:6: cannot inline (*Histogram).Observe: function too complex: cost 93 exceeds budget 80
18-pgo/ingest.go:115:6: cannot inline (*Aggregator).Ingest: unhandled op DEFER
18-pgo/ingest.go:29:7: &Record{} escapes to heap
```

Las dos funciones están justo por encima del budget, y como `parseRecord` es una llamada real, su `&Record{}` escapa: **una asignación en el heap por línea**.

### Paso 1: Mide sin PGO

Desde `labs/`:

```bash
go test -pgo=off -run '^$' -bench Ingest -benchmem -count 10 ./18-pgo/ | tee old.txt
```

### Paso 2: Recoge un profile

El profile tiene que salir de una carga de trabajo representativa. Aquí, esa carga es el propio benchmark:

```bash
go test -run '^$' -bench Ingest -cpuprofile cpu.prof ./18-pgo/
go tool pprof -top cpu.prof | head -12
```

```
      flat  flat%   sum%        cum   cum%
     0.45s 32.14% 32.14%      0.93s 66.43%  labs/18-pgo.parseRecord
     0.28s 20.00% 52.14%      0.48s 34.29%  runtime.mallocgcTinySC2
     0.21s 15.00% 67.14%      0.21s 15.00%  labs/18-pgo.(*Histogram).Observe
     0.14s 10.00% 77.14%      1.28s 91.43%  labs/18-pgo.(*Aggregator).Ingest
     0.06s  4.29% 81.43%      0.06s  4.29%  runtime.getMCache (inline)
```

Cerca de un tercio del tiempo se va en `runtime.mallocgcTinySC2`, reservando los `Record` de 12 bytes (16 bytes cada uno en el tiny allocator). Fíjate en que no hay ningún `newobject` por medio: desde Go 1.27 el compilador llama directamente a una rutina de asignación especializada por tamaño para los objetos pequeños, lo que hace cada asignación más barata que en Go 1.26, pero no la elimina.

### Paso 3: Mira qué hace el compilador con él

```bash
go build -pgo=cpu.prof -gcflags='labs/18-pgo=-m -d=pgodebug=1' ./18-pgo/ 2>&1 \
  | grep -E 'hot|PGO|parseRecord|Observe|Record\{\}'
```

```
18-pgo/ingest.go:125:16: PGO devirtualizing interface call a.obs.Observe to (*Histogram).Observe
hot-callsite-thres-from-CDF=0.6369426751592357
hot-node enabled increased budget=2000 for func=labs/18-pgo.parseRecord
18-pgo/ingest.go:28:6: can inline parseRecord
hot-node enabled increased budget=2000 for func=labs/18-pgo.(*Histogram).Observe
18-pgo/ingest.go:70:6: can inline (*Histogram).Observe
18-pgo/ingest.go:100:6: can inline (*Counter).Observe
hot-node enabled increased budget=2000 for func=labs/18-pgo.(*Aggregator).Ingest
hot-budget check allows inlining for call labs/18-pgo.parseRecord (cost 101) at 18-pgo/ingest.go:120:27 in function labs/18-pgo.(*Aggregator).Ingest
18-pgo/ingest.go:120:27: inlining call to parseRecord
hot-budget check allows inlining for call labs/18-pgo.(*Histogram).Observe (cost 93) at 18-pgo/ingest.go:125:16 in function labs/18-pgo.(*Aggregator).Ingest
18-pgo/ingest.go:125:16: inlining call to (*Histogram).Observe
18-pgo/ingest.go:29:7: &Record{} escapes to heap
18-pgo/ingest.go:120:27: &Record{} does not escape
```

Léelo línea a línea:

1. La llamada de interfaz se ha **desvirtualizado** a `(*Histogram).Observe`.
2. Las dos funciones llamadas se han **hecho inline con el budget caliente** (101 y 93 > 80).
3. Las dos últimas líneas son la verdadera recompensa. La versión independiente de `parseRecord` sigue reservando memoria (`ingest.go:29`), pero **la copia hecha inline dentro de `Ingest` no** (`ingest.go:120`). Una vez que el cuerpo está inline, el análisis de escape ve que el registro solo se copia hacia `Observe` y nunca sale de `Ingest`, así que vive en el stack. Es la interacción entre inlining y análisis de escape de [Inlining y Análisis de Escape](07-inlining-escape.md), ahora desbloqueada por el profile.

### Paso 4: Mide con PGO

```bash
go test -pgo=cpu.prof -run '^$' -bench Ingest -benchmem -count 10 ./18-pgo/ | tee new.txt
benchstat old.txt new.txt
```

En un AMD Ryzen AI MAX+ 395:

```
          │   old.txt   │               new.txt               │
          │   sec/op    │   sec/op     vs base                │
Ingest-32   1.979m ± 3%   1.004m ± 1%  -49.28% (p=0.000 n=10)

          │   old.txt    │                new.txt                │
          │     B/s      │      B/s       vs base                │
Ingest-32   563.5Mi ± 3%   1111.1Mi ± 1%  +97.17% (p=0.000 n=10)

          │   old.txt   │             new.txt                │
          │  allocs/op  │ allocs/op  vs base                 │
Ingest-32   100.0k ± 0%   0.0k ± 0%  -100.00% (p=0.000 n=10)
```

**El doble de rápido, y las 100.000 asignaciones por ejecución pasan a ser cero**, sin tocar el código fuente.

### Paso 5: El flujo de trabajo real, con `default.pgo`

En producción haces un profile del binario real (por ejemplo a través de `net/http/pprof`, durante 30 segundos o más con una carga típica), haces commit del profile como `default.pgo` junto a `main.go`, y un `go build` normal lo usa. Pruébalo:

```bash
go run ./18-pgo/cmd/ingest -cpuprofile /tmp/ingest.prof
cp /tmp/ingest.prof 18-pgo/cmd/ingest/default.pgo
go build -o /tmp/ingest-pgo ./18-pgo/cmd/ingest
go build -pgo=off -o /tmp/ingest ./18-pgo/cmd/ingest
go version -m /tmp/ingest-pgo | grep pgo
/tmp/ingest; /tmp/ingest-pgo
```

```
	build	-pgo=/home/you/.../labs/18-pgo/cmd/ingest/default.pgo
ingested 50000000 lines in 987ms (19.7 ns/line)
ingested 50000000 lines in 509ms (10.2 ns/line)
```

El binario con PGO ocupaba 8,4 KB más (2,996 MB → 3,004 MB, +0,3%). Borra `18-pgo/cmd/ingest/default.pgo` cuando termines, para que los demás pasos empiecen desde un estado limpio.

## Entendiendo los Resultados

**¿Dónde estaba el tiempo?** No principalmente en las llamadas eliminadas. Aislemos la desvirtualización desactivándola pero manteniendo el profile:

```bash
go test -pgo=cpu.prof -gcflags='labs/18-pgo=-d=pgodevirtualize=0' -run '^$' -bench Ingest -benchmem -count 10 ./18-pgo/
```

```
          │   old.txt   │            nodevirt.txt             │               new.txt               │
          │   sec/op    │   sec/op     vs base                │   sec/op     vs base                │
Ingest-32   1.979m ± 3%   1.043m ± 4%  -47.31% (p=0.000 n=10)   1.004m ± 1%  -49.28% (p=0.000 n=10)
```

Casi toda la ganancia viene de hacer inline `parseRecord`, y en concreto de la **asignación que ha eliminado**: `mallocgcTinySC2` era ~34% del profile. Desvirtualizar `Observe` solo añadió ~4% más (`-3.74%, p=0.000` frente a `nodevirt` con la máquina tranquila, y dentro del ruido con la máquina ocupada). La llamada indirecta siempre va al mismo destino, así que el predictor de saltos de la CPU ya la maneja bien (mira [Predicción de Saltos](12-branch-prediction.md)). La desvirtualización compensa sobre todo cuando el cuerpo hecho inline permite más optimizaciones, y ese no era el caso de este `Observe`.

**¿Por qué tiene `Ingest` un `defer`?** No es solo por el lock. Las decisiones sobre call sites calientes se indexan por *(llamador, desplazamiento de línea dentro del llamador)*. Si `Ingest` fuera lo bastante pequeña, se haría inline en caliente dentro de `BenchmarkIngest`. La llamada a `parseRecord` dentro de esa copia inline pertenecería entonces a otro llamador, no coincidiría con el profile, y se compilaría como una llamada normal. Así funciona PGO con la granularidad de los call sites: el efecto depende de la *forma* de tu grafo de llamadas, y `-d=pgodebug=1` es cómo averiguas qué ha pasado.

**Desplazamientos de línea, no números de línea.** El profile guarda los call sites como desplazamientos desde el principio de la función (`NodeLineOffset` en `pgoir/irgraph.go`). Editar otras funciones no invalida un profile, pero editar *dentro* de una función caliente desplaza sus call sites hasta que recojas uno nuevo.

## PGO en la Práctica

- **Las ganancias típicas son de un 2–14%** (según las mediciones del propio equipo de Go en la [guía de PGO](https://go.dev/doc/pgo)). El 2x de aquí sale de un laboratorio diseñado para dar en el punto justo: una asignación caliente que solo desaparece cuando una función se hace inline.
- **Los profiles representativos importan.** Un profile de una prueba de carga que ejercita el camino equivocado hace calientes las cosas equivocadas. Puedes combinar profiles de varias instancias con `go tool pprof -proto a.prof b.prof > merged.prof`.
- **Itera.** No hay problema en hacer un profile de un binario compilado con PGO y usar ese profile para el siguiente build. El equipo de Go lo diseñó para que fuera estable a lo largo de las iteraciones.
- **Coste del build.** Los builds con PGO tardan más y recompilan todas las dependencias con el profile. El comando go cachea el profile preprocesado (`go tool preprofile`) para ayudar.
- **No sustituye a arreglar el código.** Sacar la asignación a mano (devolviendo `Record` por valor) conseguiría la misma mejora en cualquier build. PGO es estupendo para código que no puedes o no quieres reestructurar, como las dependencias y la librería estándar.

## Puntos Clave

- PGO le da al compilador un profile de CPU. En Go 1.27 lo usa para el **inlining de call sites calientes** (budget 80 → 2000), la **desvirtualización** de llamadas a interfaces y a valores de función, y la **alineación de loops calientes** en amd64
- `-pgo=auto` usa el `default.pgo` que está junto a un paquete **main**. Los tests y las librerías necesitan `-pgo=file`
- `-gcflags=-m -d=pgodebug=1` muestra cada decisión de PGO
- El inlining es un facilitador: las mayores ganancias vienen de lo que el inlining desbloquea, como que **el análisis de escape mantenga valores en el stack**
- Las decisiones se toman por call site, así que la forma del grafo de llamadas importa, y el profile tiene que ser representativo

## Para Ir Más Allá (opcional)

1. Cambia `parseRecord` para que devuelva `Record` por valor, y haz el benchmark con `-pgo=off`. ¿Cuánto te acercas al build con PGO?
2. Cambia `observer` en `lab_test.go` a `&Counter{}`, recoge un profile nuevo, y comprueba con `-d=pgodebug=1` qué tipo se desvirtualiza ahora. ¿Qué pasa si haces el build con el profile *antiguo* y ejecutas con el nuevo observer? (La variable de entorno `INGEST_COUNT_ONLY=1` hace lo mismo para `cmd/ingest`.)
3. Prueba `-gcflags='labs/18-pgo=-d=pgoinlinebudget=100'` y `-d=pgoinlinecdfthreshold=50`. ¿Qué decisiones cambian?
4. Quita el `defer` de `Ingest` (haz el unlock a mano en cada return), vuelve a compilar con `-pgo=cpu.prof -gcflags=-m`, y comprueba si `&Record{}` sigue quedándose en el stack.
