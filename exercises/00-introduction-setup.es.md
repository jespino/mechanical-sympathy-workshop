# Introducción y Configuración

> **¿Quieres saber más?** El término viene del piloto Jackie Stewart («no hace falta ser ingeniero para ser piloto de carreras, pero sí hay que tener simpatía mecánica») y Martin Thompson lo trajo al mundo del software. Su [blog Mechanical Sympathy](https://mechanical-sympathy.blogspot.com/) sigue siendo una lectura excelente.

**Simpatía mecánica** significa escribir software entendiendo la máquina sobre la que se ejecuta. No luchas contra ella, colaboras con ella. En Go, «la máquina» son en realidad tres capas, una encima de otra:

1. **El compilador**, que decide qué vive en el stack, qué se hace inline y qué comprobaciones se pueden eliminar
2. **El runtime**, que reserva memoria, ejecuta el garbage collector y planifica las goroutines sobre los hilos
3. **El hardware**, con sus cachés, líneas de caché, predictores de saltos y el coste de entrar en el kernel

![Las tres capas bajo tu código Go: compilador, runtime y hardware](images/00-layers.webp)

La mayor parte del tiempo Go te oculta todo esto, y eso es una virtud. Pero cuando un trozo de código es caliente, la diferencia entre un código que trabaja *con* estas capas y uno que trabaja *contra* ellas suele ser de 2x, 10x o más, normalmente sin cambiar el algoritmo.

## Cómo Funciona Este Workshop

El workshop va en **crescendo**. Empezamos con las herramientas para medir, seguimos con las optimizaciones más sencillas y vamos bajando hacia la máquina a medida que avanzamos:

| Parte | Módulos | Qué aprenderás |
|---|---|---|
| I · Primero, medir | 0–2 | Benchmarks que no mienten, `benchstat` y `pprof` |
| II · Asignaciones y el compilador | 3–8 | Pre-asignación, strings, escape analysis, inlining, interfaces |
| III · La memoria y el hardware | 9–12 | Layout de structs, cachés de CPU, bounds checks, predicción de saltos |
| IV · El GC y el runtime | 13–17 | `sync.Pool`, presión sobre el GC, contención, el scheduler, syscalls |
| V · Juntándolo todo | 18–19 | PGO y un proyecto final de optimización |

Todos los módulos siguen el mismo ciclo, que es también el que deberías usar en la vida real:

![El ciclo del workshop: medir, entender por qué, cambiar una cosa, volver a medir](images/00-loop.webp)

El paso central es el corazón de cada módulo. Abrimos el código fuente de Go (el compilador, el runtime, la librería estándar) y miramos el mecanismo que hace lenta la versión lenta. Cuando entiendes el *por qué*, la solución suele ser evidente, y reconocerás el mismo patrón en código que no has visto nunca.

> **Una advertencia.** Todo en este workshop trata sobre código *caliente*. La mayoría del código no lo es. Por defecto gana la legibilidad, y solo la cambias por velocidad donde un perfil te diga que merece la pena. La Parte I existe precisamente para que nunca optimices a ciegas.

## Paso 1: Instala Go 1.27

Necesitas **Go 1.27 o superior**. Los internos que leemos son los de la versión 1.27, y algunos módulos dependen de funcionalidades recientes (`b.Loop`, maps con Swiss Tables, el GC Green Tea, la asignación especializada por tamaño).

Descárgalo de [go.dev/dl](https://go.dev/dl/) o usa tu gestor de paquetes, y comprueba:

```bash
go version
```

```
go version go1.27.3 linux/amd64
```

Cualquier versión 1.27.x vale. **No** necesitas compilar Go desde el código fuente: todo el workshop usa el toolchain estándar.

> **¿Tienes un Go más antiguo?** Con el valor por defecto `GOTOOLCHAIN=auto`, cualquier Go desde la 1.21 cambia solo a la versión correcta: `labs/go.mod` dice `go 1.27`, así que el primer comando `go` que ejecutes dentro de `labs/` descarga Go 1.27 y lo usa a partir de entonces. Compruébalo con `cd labs && go version`.

Echa un vistazo a dónde vive el código fuente de Go. Leeremos ficheros de ahí en todos los módulos:

```bash
ls $(go env GOROOT)/src/runtime | head
```

## Paso 2: Descarga el Código del Workshop

```bash
git clone <workshop-repo-url> mechanical-sympathy
cd mechanical-sympathy
```

El directorio interesante es `labs/`, un único módulo de Go con un paquete por cada módulo del workshop:

```
labs/
  go.mod
  03-preallocation/
    before.go      la versión ingenua (la referencia, no la edites)
    after.go       TU versión: empieza siendo una copia de before.go
    solution.go    una solución de referencia (solo se compila con -tags solution)
    lab_test.go    tests de corrección + benchmarks
  ...
```

Las convenciones son las mismas en todos los laboratorios:

- `before.go` y `after.go` contienen las mismas funciones con los sufijos `Before` / `After`. Solo editas `after.go`.
- `after.go` tiene la restricción de compilación `//go:build !solution` y `solution.go` tiene `//go:build solution`, así que se compila exactamente uno de los dos. Así puedes comparar tu intento con la referencia sin mover ficheros.
- Cada benchmark tiene dos sub-benchmarks, `impl=before` e `impl=after`, para que `benchstat` los muestre uno al lado del otro.
- Algunos laboratorios (benchmarking, profiling, PGO) necesitan una estructura algo distinta, y su página lo explica.

## Paso 3: Instala las Herramientas

### benchstat (obligatorio)

`benchstat` convierte varias ejecuciones de un benchmark en estadísticas: medianas, intervalos de confianza y p-valores. Lo usaremos en todos los laboratorios.

```bash
go install golang.org/x/perf/cmd/benchstat@latest
benchstat -h
```

Asegúrate de que `$(go env GOPATH)/bin` está en tu `PATH`.

### Graphviz (opcional)

`go tool pprof -http=:` funciona sin él, pero la vista de grafo necesita `dot`:

```bash
# Debian/Ubuntu
sudo apt install graphviz
# macOS
brew install graphviz
```

### perf (opcional, solo Linux)

En la Parte III usamos contadores hardware de rendimiento (fallos de caché, fallos de predicción de saltos) para *demostrar* lo que pasa dentro de la CPU. La herramienta es `perf`:

```bash
# Debian/Ubuntu
sudo apt install linux-tools-common linux-tools-$(uname -r)
# Fedora
sudo dnf install perf
```

Muchas distribuciones no dejan leer los contadores hardware a usuarios normales por defecto. Si `perf stat true` falla con un error de permisos, relaja la restricción mientras dure el workshop:

```bash
cat /proc/sys/kernel/perf_event_paranoid    # 3 o 4 significa «bloqueado»
sudo sysctl kernel.perf_event_paranoid=1
```

> ⚠️ **¿No usas Linux?** `perf` solo existe en Linux. En macOS o Windows (y dentro de muchas VMs y contenedores, que no exponen los contadores hardware), sáltate los pasos con `perf`. Todos los módulos funcionan también solo con benchmarks. Verás el efecto en `sec/op`, simplemente no verás el contador que lo explica. En macOS, la plantilla «CPU Counters» de Instruments puede mostrar datos parecidos si quieres profundizar.

## Paso 4: Verifica Tu Entorno

Desde la raíz del repositorio, comprueba que todos los laboratorios compilan y pasan sus tests, tanto en tu versión como en las soluciones de referencia:

```bash
make test-labs
```

Después, lanza tu primera comparación de benchmarks con la solución de referencia del laboratorio de pre-asignación:

```bash
make solution LAB=03
```

En unos segundos deberías ver una tabla de `benchstat` parecida a esta, con porcentajes muy negativos en la columna `vs base`:

```
              │    before     │                after                │
              │    sec/op     │    sec/op     vs base               │
Squares-32      28.684µ ± 30%   6.690µ ± 13%  -76.68% (p=0.002 n=6)
Evens-32        13.415µ ±  6%   7.069µ ±  7%  -47.31% (p=0.002 n=6)
CountWords-32    252.8µ ± 17%   152.6µ ± 10%  -39.63% (p=0.002 n=6)
```

Si funciona, ya estás listo. No te preocupes por entender la tabla todavía: de eso se encarga el siguiente módulo.

## Paso 5: Haz Que Tu Máquina Sea Aburrida

Los benchmarks miden tu código *más todo lo demás que esté haciendo tu máquina*. Para conseguir números estables:

- **Enchufa el portátil.** Con batería, las CPUs reducen su frecuencia de forma agresiva.
- **Cierra aplicaciones pesadas**: navegadores con muchas pestañas, indexadores del IDE, videollamadas, builds de Docker.
- **No toques la máquina** mientras se ejecuta un benchmark largo.
- **En Linux, plantéate usar el governor `performance`** mientras haces benchmarks: `sudo cpupower frequency-set -g performance`.
- **Mejor en bare metal.** Las VMs y los runners de CI comparten CPU con vecinos ruidosos.

Nunca tendrás cero ruido, y por eso siempre ejecutaremos los benchmarks varias veces y dejaremos que `benchstat` nos diga si una diferencia es real.

## Referencia de Comandos Útiles

| Comando | Qué hace |
|---|---|
| `make bench LAB=NN` | Compara tu `after.go` con `before.go` |
| `make solution LAB=NN` | Lo mismo, usando la referencia `solution.go` |
| `make bench LAB=NN COUNT=10` | Más repeticiones, para menos ruido |
| `make bench LAB=NN BENCH=Squares` | Ejecuta solo los benchmarks que casen con una regexp |
| `make test-labs` | Comprueba que todos los laboratorios compilan y pasan sus tests |
| `go build -gcflags=-m ./NN-nombre/` | Pregunta al compilador por sus decisiones de inlining y escape |
| `go tool pprof -http=: cpu.prof` | Explora un perfil en el navegador |

## Puntos Clave

- La simpatía mecánica consiste en colaborar con el **compilador**, el **runtime** y el **hardware**
- Todos los módulos siguen el ciclo **medir → entender por qué → cambiar una cosa → volver a medir**
- Editas `after.go`, comparas con `before.go` y puedes mirar `solution.go` con `-tags solution`
- Optimiza solo lo que una medición te diga que es caliente, y mantén tu máquina tranquila mientras mides
