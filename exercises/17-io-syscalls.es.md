# I/O y Syscalls

> **¿Quieres saber más?** Lee `entersyscall`, `exitsyscall` y `retake` en [`runtime/proc.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/proc.go), `(*FD).Write` en [`internal/poll/fd_unix.go`](https://github.com/golang/go/blob/go1.27.0/src/internal/poll/fd_unix.go), `newFile` en [`os/file_unix.go`](https://github.com/golang/go/blob/go1.27.0/src/os/file_unix.go), y la documentación del paquete [`bufio`](https://pkg.go.dev/bufio).

Hasta ahora, todos los costes que hemos visto vivían dentro de tu proceso: asignaciones, fallos de caché, saltos, locks. Este módulo trata del coste de salir de él. Cada vez que tu programa lee o escribe un fichero, le pide al kernel que lo haga, y cruzar esa frontera es una de las "operaciones individuales" más caras que realiza un programa en Go. Veremos cuánto cuesta una syscall, qué hace el scheduler de Go mientras hay una en curso, y por qué poner un buffer a tu I/O suele ser la mayor mejora de una sola línea que existe.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Sabrás cuánto cuesta una syscall comparada con una llamada a función, y por qué
- Entenderás qué hacen `entersyscall` / `exitsyscall` y cómo `sysmon` entrega a otro thread una P atascada en una syscall
- Conocerás la diferencia entre la I/O de red (netpoller) y la I/O de ficheros (syscalls bloqueantes) en Go
- Sabrás que `*os.File` **no tiene buffer**, y usarás `bufio.Writer` / `bufio.Reader` para agrupar syscalls
- Contarás syscalls con `strace -c` y las reconocerás en un perfil de CPU
- Evitarás el error clásico de `bufio`: olvidarse de hacer `Flush`

## El Concepto: Cruzando al Kernel

### Cuánto cuesta una syscall

Una syscall no es una llamada a función. En x86-64, la instrucción `SYSCALL` cambia la CPU a modo kernel, el kernel guarda los registros de usuario, comprueba el número de syscall, ejecuta el manejador, copia datos entre la memoria de usuario y la del kernel, y después vuelve con `SYSRET`. Además, las mitigaciones de vulnerabilidades de CPU (Spectre y compañía) pueden añadir trabajo en la frontera. Puedes ver cuáles aplica tu máquina en `/sys/devices/system/cpu/vulnerabilities/`.

El laboratorio incluye un pequeño programa que mide el coste directamente:

```bash
cd labs
go run ./17-io-syscalls/cmd/syscallcost
```

```
function call (noinline)                  1.8 ns/op
bufio.Writer.Write 1 byte                 3.3 ns/op
getpid via RawSyscall                    39.5 ns/op
getpid via Syscall (+entersyscall)       52.6 ns/op
os.File.Write 1 byte to /dev/null       100.6 ns/op
```

En un AMD Ryzen AI MAX+ 395, la syscall más barata posible (`getpid`, que no hace nada) cuesta unas **20x una llamada a función**. Un `write` real de un byte a `/dev/null`, que el kernel tira a la basura, cuesta ~100 ns, unas **30x más que copiar ese byte en un buffer de `bufio`**. La diferencia entre las dos líneas de `getpid` es la contabilidad propia de Go, que veremos a continuación.

### Qué hace Go alrededor de una syscall

Cuando tu goroutine llama a `syscall.Write`, acaba en `syscall.Syscall` (`syscall/syscall_linux.go`):

```go
func Syscall(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err Errno) {
	runtime_entersyscall()
	...
	r1, r2, err = RawSyscall6(trap, a1, a2, a3, 0, 0, 0)
	runtime_exitsyscall()
	return
}
```

`runtime.entersyscall` (`reentersyscall` en `runtime/proc.go`) guarda el stack pointer y el PC de la goroutine y la cambia de `_Grunning` a `_Gsyscall`. La M (thread del sistema operativo) conserva su P, pero a partir de ahora **le pueden quitar la P**:

```go
// As soon as we switch to _Gsyscall, we are in danger of losing our P.
// We must not touch it after this point.
```

¿Quién se la quita? `sysmon`, el thread monitor del runtime, que se ejecuta sin P y se despierta cada 20µs a 10ms. En `retake` examina cada P cuya goroutine está en una syscall:

```go
// Retake the P if it's there for more than 1 sysmon tick (at least 20us).
...
// On the one hand we don't want to retake Ps if there is no other work to do,
// but on the other hand we want to retake them eventually
// because they can prevent the sysmon thread from deep sleep.
if runqempty(pp) && sched.nmspinning.Load()+sched.npidle.Load() > 0 && pd.syscallwhen+10*1000*1000 > now {
	thread.resume()
	goto done
}
...
handoffp(pp)
```

Así que una syscall que lleva ejecutándose al menos un tick de sysmon pierde su P en favor de otro thread (`handoffp`) **si hay otro trabajo esperando**, o tras 10ms en cualquier caso. Así es como una goroutine atascada en un `read` lento no congela una P entera. Cuando la syscall vuelve, `exitsyscall` toma el camino rápido si la P sigue ahí. Si no, tiene que encontrar una P libre, o aparcar la goroutine en la cola de ejecución global.

La mayoría de las syscalls son cortas, así que solo pagan el camino rápido: ~10 ns de contabilidad además del cruce al kernel. Ten en cuenta que desde Go 1.26 ya no existe el estado `_Psyscall` (sigue siendo `_Psyscall_unused` en el `runtime2.go` de Go 1.27). Si una P está "en una syscall" se decide mirando el estado de su goroutine.

### Red frente a ficheros: el netpoller

No toda la I/O bloquea un thread. Los sockets y las pipes se ponen en **modo no bloqueante** y se registran en el **netpoller** (epoll en Linux, kqueue en BSD/macOS). Mira `(*FD).Write` en `internal/poll/fd_unix.go`:

```go
n, err := ignoringEINTRIO(syscall.Write, fd.Sysfd, p[nn:max])
...
if err == syscall.EAGAIN && fd.pd.pollable() {
	if err = fd.pd.waitWrite(fd.isFile); err == nil {
		continue
	}
}
```

Si un socket no está listo, la syscall devuelve `EAGAIN` de inmediato y la goroutine **se aparca** (sin bloquear ningún thread), y el netpoller la despertará más tarde. Los ficheros normales son distintos: epoll los rechaza, y `newFile` en `os/file_unix.go` indica que registrar "disk files on Linux systems" falla. Así que una lectura o escritura de fichero es una simple **syscall bloqueante** que retiene una M durante toda su duración. Ese es el caso en el que importa el traspaso de `sysmon` que vimos arriba.

En cualquier caso, **cada** `Read` o `Write` sobre un `*os.File` o un `net.Conn` es al menos una syscall. `*os.File` no tiene buffer. `fmt.Fprintf(f, ...)` formatea en un buffer temporal y luego llama a `f.Write` una vez, así que cuesta **una syscall por llamada**. Lo mismo pasa con `fmt.Println`, que escribe en `os.Stdout`, un `*os.File`.

### Agrupando con bufio

`bufio.Writer` mantiene un buffer (`defaultBufSize = 4096` en `bufio/bufio.go`) y solo llama al `Write` subyacente cuando está lleno:

```go
func (b *Writer) Write(p []byte) (nn int, err error) {
	for len(p) > b.Available() && b.err == nil {
		if b.Buffered() == 0 {
			// Large write, empty buffer.
			// Write directly from p to avoid copy.
			n, b.err = b.wr.Write(p)
		} else {
			n = copy(b.buf[b.n:], p)
			b.n += n
			b.Flush()
		}
		...
	}
	...
	n := copy(b.buf[b.n:], p)
```

![Sin buffer, cada escritura pequeña cruza al kernel; bufio las acumula y cruza una vez cada 4 KB](images/17-syscalls.webp)

Un registro de 20 bytes se convierte en un `copy` en memoria. Ahora una sola syscall transporta ~200 registros. `bufio.Reader` y `bufio.Scanner` hacen lo mismo con las lecturas. Dos cosas que recordar:

- **Tienes que llamar a `Flush`.** Lo que siga en el buffer cuando pares se pierde, sin error y sin aviso.
- **Los errores son persistentes.** Tras una escritura fallida, `b.err` queda establecido y todos los `Write` y `Flush` posteriores lo devuelven. Así que basta con comprobar el error del `Flush` final, pero un `defer w.Flush()` que ignora el error no basta.

## El Laboratorio

```
labs/17-io-syscalls/
  before.go          implementaciones ingenuas (referencia, no lo edites)
  after.go           tu copia: haz que esta sea rápida
  solution.go        solución de referencia (se compila con -tags solution)
  lab_test.go        test de corrección + benchmarks
  cmd/syscallcost/   la demostración del coste de syscalls de arriba (solo Linux)
  cmd/records/       escribe registros en un fichero, para strace
```

| Función | Lo que hace |
|---|---|
| `WriteRecords(f, recs)` | Escribe 5.000 líneas CSV (`id,name,latency`) en un `*os.File` con `fmt.Fprintf` |
| `CountLines(r)` | Cuenta las líneas de un fichero de ~93 KB, leyendo **un byte por cada `Read`** |

El benchmark de escritura escribe en `/dev/null`, lo que sigue costando una syscall `write` completa pero sin I/O de disco. El benchmark de lectura lee un fichero temporal real que se queda en el page cache. Ambos miden el coste de las syscalls, no tu SSD.

### Paso 1: Mide la línea base

```bash
make bench LAB=17
```

Las dos columnas deberían ser iguales (`~`). Fíjate en los números absolutos: casi 1ms para escribir 93 KB es muy lento.

### Paso 2: Cuenta las syscalls

Compila el programa `records` y ejecútalo bajo `strace -c`, que cuenta las syscalls por tipo (`-f` sigue a todos los threads del runtime):

```bash
cd labs
go build -o /tmp/records ./17-io-syscalls/cmd/records
strace -c -f -e trace=write /tmp/records -mode=before
```

```
mode=before: wrote 5000 records, file is 93331 bytes (/tmp/records.csv)
% time     seconds  usecs/call     calls    errors syscall
------ ----------- ----------- --------- --------- ----------------
100,00    0,002706           0      5001           write
```

**5.001 llamadas a `write`**: una por registro, más el mensaje final a stderr.

> **¿No estás en Linux?** En macOS, `dtruss` necesita relajar System Integrity Protection, así que sáltate este paso y apóyate en el perfil y el benchmark. En Windows, usa también el perfil.

### Paso 3: Encuentra las syscalls en un perfil

```bash
go test -run '^$' -bench 'WriteRecords/impl=before' -cpuprofile cpu.prof ./17-io-syscalls/
go tool pprof -top -cum cpu.prof | head -25
```

```
      flat  flat%   sum%        cum   cum%
     0.02s  1.65%  1.65%      1.15s 95.04%  labs/17-io-syscalls.WriteRecordsBefore
     0.03s  2.48%  4.13%      1.01s 83.47%  fmt.Fprintf
     0.01s  0.83%  4.96%      0.66s 54.55%  os.(*File).Write
     0.01s  0.83%  5.79%      0.65s 53.72%  internal/poll.(*FD).Write
     0.01s  0.83%  6.61%      0.56s 46.28%  syscall.write
     0.01s  0.83%  7.44%      0.55s 45.45%  syscall.Syscall
     0.03s  2.48%  9.92%      0.44s 36.36%  syscall.RawSyscall6
     0.41s 33.88% 43.80%      0.41s 33.88%  internal/runtime/syscall/linux.Syscall6
     0.02s  1.65% 48.76%      0.21s 17.36%  fmt.(*pp).printArg
     0.03s  2.48% 57.85%      0.10s  8.26%  runtime.mallocgc
     0.01s  0.83% 67.77%      0.08s  6.61%  runtime.convT64
     0.02s  1.65% 83.47%      0.05s  4.13%  runtime.mallocgcTinySC2
     0.01s  0.83% 85.12%      0.04s  3.31%  runtime.convTstring
```

La cadena `os.(*File).Write → internal/poll.(*FD).Write → syscall.Syscall → syscall.RawSyscall6 → internal/runtime/syscall/linux.Syscall6` es la firma de una escritura sin buffer. El gran tiempo **flat** en `Syscall6` es tiempo pasado dentro del kernel: el profiler de CPU mide el tiempo de CPU del thread, incluido el tiempo de kernel. Fíjate también en `convTstring` y `convT64`: `fmt.Fprintf` empaqueta cada argumento en un `any`, y eso asigna memoria (mira [Interfaces y Boxing](08-interfaces-boxing.md)). `mallocgcTinySC2` es nuevo en Go 1.27: `mallocgc` ahora reparte las asignaciones pequeñas entre rutinas especializadas por tamaño, así que verás nombres como `mallocgcTinySC2` o `mallocgcSmallScanNoHeaderSC2` debajo de él.

### Paso 4: Arréglalo

Edita `labs/17-io-syscalls/after.go`:

1. **`WriteRecordsAfter`**: envuelve `f` en `bufio.NewWriter(f)`, escribe en el buffer y haz `return w.Flush()` al final. Ejecuta el benchmark: esto por sí solo elimina el coste de las syscalls.
2. **Extra**: sustituye `fmt.Fprintf` por `strconv.AppendInt` sobre un `line []byte` que reutilizas (`line[:0]`) para cada registro. Esto elimina también las asignaciones del boxing.
3. **`CountLinesAfter`**: lee en un buffer grande (`make([]byte, 32*1024)`) y cuenta con `bytes.Count(buf[:n], []byte{'\n'})`. Envolver `r` en un `bufio.Reader` y llamar a `ReadByte` también funciona, pero es más lento.

Ejecuta `go test ./17-io-syscalls/`: el test compara los ficheros byte a byte, así que si falta un `Flush`, falla.

### Paso 5: Compara

```bash
make bench LAB=17
```

Este es el paso intermedio, solo con `bufio.NewWriter` añadido y `fmt.Fprintf` todavía en su sitio, en un AMD Ryzen AI MAX+ 395:

```
                │   before    │               after                │
                │   sec/op    │   sec/op     vs base               │
WriteRecords-32   885.0µ ± 6%   392.2µ ± 2%  -55.68% (p=0.000 n=8)
```

Y la solución de referencia (`make solution LAB=17`):

```
                │     before      │               after                │
                │     sec/op      │   sec/op     vs base               │
WriteRecords-32      855.89µ ± 2%   86.35µ ± 1%  -89.91% (p=0.000 n=8)
CountLines-32     13431.148µ ± 4%   4.830µ ± 3%  -99.96% (p=0.000 n=8)

                │     before     │                   after                    │
                │      B/op      │      B/op       vs base                    │
WriteRecords-32   154.393Ki ± 0%     4.055Ki ± 0%       -97.37% (p=0.000 n=8)
CountLines-32         1.000 ± 0%   32768.000 ± 0%  +3276700.00% (p=0.000 n=8)

                │     before     │                after                │
                │   allocs/op    │ allocs/op   vs base                 │
WriteRecords-32   14730.000 ± 0%   4.000 ± 0%  -99.97% (p=0.000 n=8)
CountLines-32         1.000 ± 0%   1.000 ± 0%        ~ (p=1.000 n=8) ¹
```

Ejecuta `strace` de nuevo con tu nueva versión:

```bash
go build -o /tmp/records ./17-io-syscalls/cmd/records
strace -c -f -e trace=write /tmp/records -mode=after
```

```
100,00    0,000051           2        24           write
```

**24 escrituras en lugar de 5.001**: 22 buffers completos de 4 KiB, un `Flush` final de los 3.219 bytes restantes, y el mensaje a stderr.

## Entendiendo los Resultados

**WriteRecords** se divide limpiamente en dos costes. Añadir `bufio` redujo el tiempo más o menos a la mitad (~885µs → ~392µs). Eso son ~5.000 syscalls × ~100ns. Los ~400µs restantes eran de `fmt`: analizar el string de formato, empaquetar tres argumentos por registro (~14.700 asignaciones) e imprimir mediante reflexión. `strconv.AppendInt` sobre un slice reutilizado eliminó eso, para un total de **~10x**. Solo quedan 4 asignaciones: el `bufio.Writer`, su buffer de 4 KiB y el slice auxiliar `line` creciendo hasta que cabe el registro más largo.

**CountLines** es el caso extremo: 93.331 syscalls `read` de un byte cada una, ~150ns cada una, frente a 4 lecturas de 32 KiB. Eso es una diferencia de **~3.000x** para el mismo resultado. B/op subió de 1 byte a 32 KiB porque reservamos el buffer en cada llamada. Aquí es un buen trato, y podrías reutilizarlo entre llamadas si importara. ¿Por qué la versión "before" asigna siquiera 1 byte? `one` escapa porque se pasa a `r.Read` a través de una interfaz, y el compilador no puede ver qué hace la implementación con él (mira [Análisis de Escape](05-escape-analysis.md)).

Prueba el modo noflush del programa de demostración para ver el error clásico:

```bash
/tmp/records -mode=noflush
```

```
mode=noflush: wrote 5000 records, file is 90112 bytes (/tmp/records.csv)
```

90.112 = 22 × 4.096. El último buffer parcial, 3.219 bytes o unos 170 registros, desapareció en silencio. Ni error, ni panic.

> **Regla práctica:** nunca hagas I/O pequeña directamente sobre un `*os.File` o un `net.Conn` dentro de un bucle. Pon delante un `bufio.Writer`/`bufio.Reader`, dimensiona el buffer según tu patrón de acceso (`bufio.NewWriterSize`), y comprueba siempre el error de `Flush`.

## Más Allá de bufio

- `os.ReadFile` / `io.ReadAll` / `io.Copy` ya usan buffers grandes, así que prefiérelos cuando trabajes con ficheros completos.
- `bufio.Scanner` lee líneas con un token máximo de 64 KiB por defecto (`MaxScanTokenSize`). Las líneas más largas fallan con `ErrTooLong` a menos que llames a `Scanner.Buffer`.
- `net/http` ya pone buffer a las respuestas (un buffer de 2 KiB por respuesta además de un `bufio.Writer` de 4 KiB por conexión, en `net/http/server.go`), así que envolver de nuevo `http.ResponseWriter` rara vez ayuda.
- Los handlers de `log.Logger` y `slog` escriben una línea por llamada en su `io.Writer`. Si ese es `os.Stderr`, cada línea de log es una syscall.
- Agrupación al estilo `writev`: `net.Buffers` envía varios slices con un solo `writev` en sockets.
- El buffering cambia latencia y durabilidad por throughput: los datos en un `bufio.Writer` (¡o en el page cache del kernel!) se pierden si el proceso (o la máquina) muere. `f.Sync()` (`fsync`) es la forma de hacerlos duraderos, y es mucho más caro que `write`.

## Puntos Clave

- Una syscall cuesta de decenas a cientos de nanosegundos, **20–50x una llamada a función**, antes de hacer ningún trabajo real
- `entersyscall` marca la goroutine como `_Gsyscall`, y `sysmon` entrega su P a otro thread si la llamada tarda más de ~20µs y hay otro trabajo
- Los sockets usan el netpoller (se aparcan, no se bloquean). Los ficheros normales son syscalls bloqueantes que retienen una M
- `*os.File` no tiene buffer: cada `Write`, `Fprintf` o `Println` es una syscall
- `bufio` convierte miles de syscalls en un puñado. `strace -c` y `syscall.Syscall` en pprof te muestran cuándo lo necesitas
- Haz siempre `Flush`, y comprueba su error

## Para Ir Más Allá (opcional)

1. Ejecuta `make bench LAB=17` con `bufio.NewWriterSize(f, 64*1024)`. ¿Ayuda un buffer más grande? ¿Cuándo deja de importar?
2. Usa `go test -bench 'WriteRecords/impl=before' -trace trace.out` y abre `go tool trace trace.out`. Encuentra los eventos de syscall en la vista de goroutines.
3. Ejecuta el benchmark bajo `/usr/bin/time -v go test ...` (o `perf stat -e task-clock,context-switches`). Compara el "System time" antes y después.
4. Cambia el benchmark de escritura para que use un fichero real en `b.TempDir()` en lugar de `/dev/null`. ¿Cuánto añade el page cache por cada `write`?
