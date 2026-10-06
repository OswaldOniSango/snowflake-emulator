# Laboratorio: del CSV a una tabla dinámica

Un recorrido local para practicar SQL, carga de archivos, streams, procedures,
tasks y roles. La prueba de navegador ejecuta estos mismos archivos; los datos
y las credenciales son exclusivamente de demostración.

## Preparación

Usa una instancia de estudio vacía. Desde la raíz del repositorio:

```bash
docker build -t snowflake-emulator:study .
docker run --rm --name snowflake-study -p 8080:8080 snowflake-emulator:study
```

Abre <http://localhost:8080> e inicia sesión con `ADMIN` / `admin`.
El laboratorio crea objetos `JOURNEY_*`: no uses esos nombres para datos propios.
Los archivos están preparados para ejecutarse **en orden y una sola vez**.
Para repetir, termina la limpieza o recrea la instancia en memoria.

Pega cada archivo SQL completo en un worksheet y pulsa **Run all**. Para mirar
resultados intermedios, pon el cursor dentro de una sentencia y pulsa **Run**.
No vuelvas a ejecutar el `COPY INTO`: una segunda carga duplica los datos.

## Recorrido

| Paso | Archivo / acción | Resultado esperado |
| --- | --- | --- |
| 1 | [01-bootstrap.sql](01-bootstrap.sql), como ADMIN | Crea la base, warehouse, dos roles y dos usuarios. No necesita warehouse previo. |
| 2 | En el selector de rol/warehouse, elige `JOURNEY_WH`; después selecciona `JOURNEY_DB` / `PUBLIC` | Contexto: ACCOUNTADMIN · JOURNEY_WH · JOURNEY_DB · PUBLIC. |
| 3 | [02-objects.sql](02-objects.sql) | Tablas, stage, stream y view. La consulta de la tabla temporal devuelve `42`. |
| 4 | Refresca **Objects**, expande JOURNEY_DB → PUBLIC y usa la flecha de carga junto a USERS_STAGE | Sube [users.csv](users.csv); debe aparecer la confirmación de carga. |
| 5 | [03-load.sql](03-load.sql) | Fuente: Ada, Grace y Linus. Ambas consultas al stream devuelven `3`: leerlo no consume el offset. La view también muestra `3`. |
| 6 | [04-pipeline.sql](04-pipeline.sql) | El procedure copia 3 filas y consume el stream. La task procesa a Barbara y deja 4 filas, sin duplicarlas cuando se ejecuta de nuevo. |
| 7 | Logout; login `JOURNEY_ALICE` / `learn-alice`. Elige warehouse y namespace de nuevo. Ejecuta [05-reader.sql](05-reader.sql). | Usuario JOURNEY_ALICE, rol JOURNEY_READER y 4 filas visibles. El panel anterior debe estar vacío al entrar. |
| 8 | [06-denied.sql](06-denied.sql), **cada sentencia por separado** | El INSERT y el intento de usar JOURNEY_WRITER deben fallar. Ese rol no debe ofrecerse a Alice en el selector. |
| 9 | Abre **History** como Alice | Solo sus sentencias; no debe aparecer el SQL ejecutado por ADMIN. |
| 10 | Logout; login `JOURNEY_BOB` / `learn-bob`. Elige contexto. Ejecuta [07-writer.sql](07-writer.sql). | Puede insertar, actualizar y borrar. La consulta intermedia dice `Updated writer`; al final quedan 4 filas. |
| 11 | Como Bob, selecciona JOURNEY_READER en el selector de roles y prueba el INSERT de 06-denied.sql | Debe fallar: el usuario tiene ambos roles, pero cuenta el rol activo. |
| 12 | Logout; login ADMIN / admin, elige el contexto y ejecuta [08-cleanup.sql](08-cleanup.sql) | Elimina objetos del laboratorio, archivo del stage, usuarios y roles. |
| 13 | Cambia a `TEST_DB` / `PUBLIC` y ejecuta [09-finish.sql](09-finish.sql) | Elimina JOURNEY_DB y JOURNEY_WH. Refresca Objects para comprobarlo. |

### Resultados del pipeline

Después del primer `CALL`: `processed_rows = 3`, `pending_rows = 0`.
La primera materialización devuelve `EU = 2`, `US = 1`.
Después de insertar a Barbara y ejecutar la task: `processed_rows = 4`,
`pending_rows = 0`, pero la tabla dinámica aún suma `3`.
Después del refresh: `EU = 2`, `US = 2`, y `procedure_calls = 2`.
La última ejecución de la task vuelve a invocar el procedure (el log pasa a 3)
pero no duplica las filas procesadas: siguen siendo 4.

La task tiene un schedule de una hora y permanece **suspendida** durante este
recorrido. `EXECUTE TASK` permite probarla inmediatamente. No se comprueba aquí
la ejecución por reloj. Los cambios de Bob se usan para practicar permisos;
no se vuelven a procesar en el pipeline append-only.

### Límites que se deben aprender correctamente

- El stream de este ejercicio es append-only; no demuestra CDC completo de UPDATE/DELETE.
- `TARGET_LAG` es metadata: las dynamic tables requieren refresh manual.
- TRANSIENT no implementa Time Travel ni Fail-safe. La tabla temporal demuestra
  creación/consulta; este ejercicio no certifica aislamiento de temporales entre sesiones.
- El warehouse simula admisión y capacidad; el tamaño no garantiza aceleración física.
- Los resultados e historial se aíslan por usuario. Los borradores SQL siguen siendo
  locales al navegador y compartidos en él; los endpoints REST de catálogo/stages
  aún no tienen el mismo control de autorización. Es una herramienta de estudio local.

## Ejecutar la prueba automática

Requisitos: Go y compilador C compatibles con `go.mod`, Node de `_web/.nvmrc`.
Activa Node con `cd _web && nvm use && cd ..` si usas nvm. Desde la raíz:

```bash
npm --prefix _web ci
npm --prefix _web run test:e2e:install
npm --prefix _web run test:e2e
```

Para ver Chromium mientras se ejecuta:

```bash
npm --prefix _web run test:e2e:headed
```

El comando compila la interfaz y el servidor, inicia una base en memoria en el
puerto **18089** y usa una carpeta temporal para los archivos. Rechaza un puerto
ocupado: nunca reutiliza la instancia donde estudias. Al finalizar se detiene el
servidor y se elimina esa carpeta temporal. No usa tu `DB_PATH` ni `STAGE_DIR`.

La prueba verifica login, selectores, carga del CSV desde la UI, resultados,
permisos denegados, cambio de rol, logout/login, historial y limpieza. Comprueba
la versión compilada que se incrusta en Go; no simula respuestas del backend.
Se ejecuta también en el job **Web Console** de CI. Ante fallos quedan reporte,
captura y traza en `_web/playwright-report` y `_web/test-results`.
