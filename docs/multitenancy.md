# Multi-tenancy

## Modelo

Base de datos y esquemas compartidos, con columna `tenant_id` en todas las tablas de negocio y **row-level security (RLS)** de PostgreSQL. Es el modelo más eficiente para muchos tenants pequeños y medianos, y la base de datos impone el aislamiento aunque haya un error en el código.

## Cómo fluye el tenant

1. **Entrada**: `grpcx.tenantInterceptor` resuelve el tenant con un `TenantResolver` y lo guarda en el `context`. Sin tenant, la respuesta es `UNAUTHENTICATED`.
2. **Aplicación**: los handlers llaman a `tenancy.FromContext`; no hay tenant por defecto.
3. **Datos**: `postgres.DB.InTenantTx` abre la transacción y ejecuta `set_config('app.tenant_id', $1, true)`, que solo dura esa transacción. No hay otra forma de acceder a tablas de tenant.
4. **Base de datos**: las políticas `tenant_isolation` filtran lecturas (`USING`) y rechazan escrituras de otro tenant (`WITH CHECK`).
5. **Eventos**: el tenant viaja en la cabecera `x-tenant-id`. `inbox.Handle` lo restaura antes de abrir la transacción del consumidor.

## Roles de base de datos

| Rol | Uso | RLS |
|---|---|---|
| `oms_owner` | Dueño del esquema; solo para migraciones | Se le aplica igualmente por `FORCE ROW LEVEL SECURITY` |
| `oms_app` | Conexión de la API y del worker; `NOBYPASSRLS` y solo `SELECT/INSERT/UPDATE` | Siempre |

Sin `app.tenant_id`, `current_tenant_id()` devuelve `NULL` y las políticas no devuelven filas. Esto lo comprueba `TestRowLevelSecurity`.

`messaging.outbox` y `messaging.inbox` no tienen RLS: son infraestructura que el relay lee para todos los tenants. La API solo inserta en ellas a través del repositorio.

## Producción: tenant autenticado

`grpcx.HeaderTenantResolver` confía en la cabecera `X-Tenant-Id`. Solo es aceptable detrás de un gateway que **ya** autentica y fija esa cabecera. Para exponer la API directamente, sustitúyelo en `cmd/api/main.go` por un resolver que valide el token y lea el claim:

```go
func JWTTenantResolver(verifier *oidc.IDTokenVerifier) grpcx.TenantResolver {
	return func(ctx context.Context) (tenancy.ID, error) {
		raw := bearerToken(ctx)                   // de la metadata "authorization"
		token, err := verifier.Verify(ctx, raw)   // firma, exp, aud, iss
		if err != nil {
			return "", tenancy.ErrMissing
		}
		var claims struct{ TenantID string `json:"tenant_id"` }
		if err := token.Claims(&claims); err != nil {
			return "", tenancy.ErrInvalid
		}
		return tenancy.Parse(claims.TenantID)
	}
}
```

La autorización por rol dentro del tenant (quién puede cancelar, por ejemplo) se añade como otro interceptor o en el handler. El dominio no conoce usuarios.

## Escalar más allá

Si un tenant necesita aislamiento físico (por normativa o por volumen), el mismo código sirve con una base de datos por tenant: el resolver elige además el pool. RLS se mantiene como segunda barrera.
