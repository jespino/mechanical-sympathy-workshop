# I/O y Syscalls

> **¿Quieres saber más?** En Internals for Interns, [El Scheduler](https://internals-for-interns.com/es/posts/go-runtime-scheduler/#system-calls) explica qué hace el runtime cuando una goroutine entra en una llamada al sistema, y [El Network Poller](https://internals-for-interns.com/es/posts/go-netpoller/) explica cómo mantiene Go una API bloqueante sobre E/S de red no bloqueante.

Leer o escribir un archivo pide trabajo al sistema operativo mediante una syscall. Miles de lecturas o escrituras diminutas pueden dedicar más tiempo a esas peticiones que al procesamiento útil. Las agruparemos usando buffers.

## Objetivos de Aprendizaje

Al terminar este módulo, podrás:

- Entender por qué son caras muchas llamadas pequeñas de I/O
- Usar escrituras con buffer y lecturas más grandes
- Contar syscalls de escritura con `strace` en Linux
- Gestionar errores de escritura y vaciar el buffer final

## El Concepto: Cruzando al Kernel

### Pasar al sistema operativo

Una syscall cede el control al kernel, que realiza una operación y devuelve el resultado. Go también gestiona la ejecución para que otras goroutines puedan continuar si la syscall bloquea. Eso requiere más trabajo que copiar unos bytes en memoria.

Los sockets suelen permitir que Go espere a que estén listos sin retener un hilo del sistema operativo. El I/O de archivos normales puede bloquear un hilo. En ambos casos, reducir llamadas innecesarias puede ayudar.

### Agrupa escrituras pequeñas

`*os.File` no agrupa las escrituras pequeñas en un buffer de Go. Un bucle que llama a `fmt.Fprintf(f, ...)` por cada registro normalmente hace una syscall de escritura por registro.

`bufio.Writer` reúne esas escrituras en memoria y envía bloques mayores al writer. Su buffer predeterminado es de 4 KiB. Leer bloques grandes aporta la misma ventaja en la dirección contraria.

![Sin buffer, cada escritura pequeña cruza al kernel; bufio las acumula y cruza una vez cada 4 KB](images/14-syscalls.webp)

### Termina la escritura

Llama siempre a `Flush` y comprueba su error. La última parte de la salida puede seguir en memoria cuando termina el bucle. Cerrar el archivo subyacente no vacía por ti un `bufio.Writer` separado.

Gestiona también los errores al escribir. `Flush` envía los bytes al writer; no garantiza que los datos del archivo estén guardados de forma duradera en disco.

## El Laboratorio

`WriteRecords` escribe 5.000 líneas CSV con `fmt.Fprintf`. `CountLines` lee un archivo byte a byte. El benchmark de escritura usa `/dev/null`; el de lectura usa un archivo temporal. Medimos sobre todo el coste de las llamadas, no la velocidad del disco.

Ejecuta los comandos desde la raíz del repositorio.

### Paso 1: Mide la línea base

```bash
make bench LAB=14
```

### Paso 2: Encuentra el coste

En Linux, cuenta las escrituras con:

```bash
(cd labs && go build -o /tmp/records ./14-io-syscalls/cmd/records)
strace -c -f -e trace=write /tmp/records -mode=before
```

Espera unas 5.001 escrituras: una por registro más el mensaje final del programa. Si no tienes `strace`, continúa con los benchmarks. La demo opcional `cmd/syscallcost` compara costes de llamadas individuales en Linux.

### Paso 3: Arréglalo

Edita `labs/14-io-syscalls/after.go`. Mantén `before.go` como referencia.

1. En `WriteRecordsAfter`, crea `bufio.NewWriter(f)` y escribe en él en lugar de hacerlo directamente en `f`. Comprueba los errores de escritura y devuelve el error del `Flush` final. Mide primero este cambio.
2. Después sustituye `fmt.Fprintf` por `strconv.AppendInt` y `append` sobre un `line []byte` reutilizado. Reinícialo con `line[:0]` en cada registro y conserva exactamente el formato CSV.
3. En `CountLinesAfter`, lee bloques en un buffer de 32 KiB y cuenta saltos de línea con `bytes.Count(buf[:n], []byte{'\n'})`. Procesa los bytes recibidos antes de gestionar `io.EOF` u otro error.

### Paso 4: Comprueba y compara

```bash
(cd labs && go test ./14-io-syscalls/)
make bench LAB=14
```

Solución de referencia (`make solution LAB=14`), en mi máquina:

| Benchmark | Antes | Después |
|---|---|---|
| `WriteRecords` | 855,89 µs | 86,35 µs |
| Asignaciones de `WriteRecords` | 14.730 | 4 |
| `CountLines` | 13,431 ms | 4,830 µs |

## Entendiendo los Resultados

**WriteRecords** mejora en dos pasos. El buffer elimina la mayoría de las syscalls pequeñas; el formato directo sobre un slice reutilizado quita el trabajo extra de `fmt` y las asignaciones por boxing. Juntos lo hacen unas 10 veces más rápido aquí.

**CountLines** hace unas pocas lecturas grandes en lugar de decenas de miles de lecturas de un byte. Usa un buffer temporal mayor, pero se coordina mucho menos con el sistema operativo.

Vuelve a compilar `/tmp/records` y ejecuta `-mode=after` con `strace`. La solución reduce las escrituras de unas 5.001 a 24. Los tests también detectan la salida que falta si olvidas `Flush`.

## Puntos Clave

- Agrupa escrituras pequeñas de archivos con `bufio.Writer`.
- Lee bloques en lugar de un byte cada vez.
- Gestiona siempre los errores y comprueba el `Flush` final.
- Usar buffers y evitar asignaciones de formato son mejoras distintas.

## Para Ir Más Allá (opcional)

1. Prueba un buffer de escritura de 64 KiB. ¿Ayuda frente al tamaño predeterminado?
2. Ejecuta `/tmp/records -mode=noflush` y compara el tamaño de la salida.
3. Reutiliza el buffer de lectura entre llamadas. ¿Cómo cambian `B/op` y `allocs/op`?
