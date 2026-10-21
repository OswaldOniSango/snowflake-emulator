# Mallard commerce showcase

Demo con datos sintéticos para explorar Mallard y tomar una captura real para redes.
Cubre los tipos de objetos soportados por el emulador; no representa todo Snowflake.

## Ejecutar

Con Mallard disponible en `http://127.0.0.1:8080` y Node.js 24 o posterior:

```sh
node examples/social-demo/run.mjs
node examples/social-demo/capture.mjs
```

Instalar primero las dependencias de `_web` y Chromium de Playwright para la captura.
Se pueden configurar `EMULATOR_URL`, `EMULATOR_USER` y `EMULATOR_PASSWORD`.
Las credenciales predeterminadas son las de desarrollo: `ADMIN` / `admin`.

El primer comando ejecuta `bootstrap.sql` sentencia por sentencia, con contexto
`MALLARD_DEMO_DB.COMMERCE` y warehouse `MALLARD_DEMO_WH`, valida el resultado de
`revenue-overview.sql` y guarda `verification.json`. Se niega a reutilizar una base
existente para evitar duplicar datos. Si hubo un fallo parcial, inspeccionar primero
lo ejecutado; `--resume-from=N` continúa desde la sentencia N sin repetir las anteriores.

La captura abre un navegador aislado, guarda worksheets de demostración en ese
navegador, ejecuta la consulta y genera `mallard-showcase.png`. No modifica los
worksheets guardados en tu navegador habitual ni publica en redes.

## Contenido

- Una base de datos, tres schemas y un warehouse X-Small.
- Nueve tablas, incluida una transient; ocho clientes, doce órdenes y seis productos.
- Cuatro vistas analíticas y dos dynamic tables.
- Tres funciones SQL escalares, tres procedimientos y tres tasks.
- Dos streams, tres stages y un rol con permisos explícitos.
- Pipeline real: stream → procedimiento → task → tabla procesada → vistas → dynamic tables.
- Resultado final de seis mercados; verificación de 12 órdenes fuente, 12 procesadas y cero cambios pendientes.

## Límites actuales

Las tasks quedan suspendidas y se ejecutan manualmente una vez con `EXECUTE TASK`.
`TARGET_LAG` programa refrescos completos para intervalos de al menos 60 segundos.
El script también refresca explícitamente para mostrar resultados inmediatos.
Los stages se crean vacíos; no se simula una conexión cloud. Las funciones se pueden
consultar con `SHOW FUNCTIONS`, aunque todavía no aparecen en el explorador.
Se usa `DECIMAL` en lugar de `NUMBER` y las llamadas a funciones requieren el
contexto `MALLARD_DEMO_DB.COMMERCE`. Usar el runner para configurar ese contexto.
