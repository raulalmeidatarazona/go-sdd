# Multi-tenancy

## Model

Shared database and schemas, a `tenant_id` column on every business table, and PostgreSQL **row-level security (RLS)**. It is the most efficient model for many small and medium tenants, and the database enforces isolation even if the code has a bug.

## How the tenant flows

1. **Entry**: `grpcx.tenantInterceptor` resolves the tenant with a `TenantResolver` and stores it in the `context`. Without a tenant the call fails with `UNAUTHENTICATED`.
2. **Application**: handlers call `tenancy.FromContext`; there is no default tenant.
3. **Data**: `postgres.DB.InTenantTx` opens the transaction and runs `set_config('app.tenant_id', $1, true)`, which lasts for that transaction only. There is no other way to reach tenant tables.
4. **Database**: the `tenant_isolation` policies filter reads (`USING`) and reject writes for other tenants (`WITH CHECK`).
5. **Events**: the tenant travels in the `x-tenant-id` header. `inbox.Handle` restores it before opening the consumer's transaction.

## Database roles

| Role | Used for | RLS |
|---|---|---|
| `oms_owner` | Schema owner; migrations only | Applies anyway thanks to `FORCE ROW LEVEL SECURITY` |
| `oms_app` | API and worker connections; `NOBYPASSRLS`, only `SELECT/INSERT/UPDATE` | Always |

Without `app.tenant_id`, `current_tenant_id()` returns `NULL` and the policies return no rows. `TestRowLevelSecurity` checks this.

`messaging.outbox` and `messaging.inbox` have no RLS: they are infrastructure the relay reads across tenants. The API only inserts into them through the repository.

## Production: authenticated tenants

`grpcx.HeaderTenantResolver` trusts the `X-Tenant-Id` header. That is only acceptable behind a gateway that **already** authenticates the caller and sets the header. To expose the API directly, replace it in `cmd/api/main.go` with a resolver that verifies the token and reads a claim:

```go
func JWTTenantResolver(verifier *oidc.IDTokenVerifier) grpcx.TenantResolver {
	return func(ctx context.Context) (tenancy.ID, error) {
		raw := bearerToken(ctx)                 // from the "authorization" metadata
		token, err := verifier.Verify(ctx, raw) // signature, exp, aud, iss
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

Role-based authorisation inside a tenant (who may cancel, for example) belongs in another interceptor or in the handler. The domain does not know about users.

## Scaling further

If a tenant needs physical isolation (regulation or volume), the same code works with one database per tenant: the resolver also selects the pool. RLS stays as a second barrier.
