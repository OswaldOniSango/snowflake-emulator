package identity

import (
	"context"
	"errors"
	"testing"
)

func TestResolveActiveRole(t *testing.T) {
	service, _, _ := testService(t)
	ctx := context.Background()
	developer, err := service.CreateRole(ctx, "DEVELOPER", "")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := service.CreateRole(ctx, "READER", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateUser(ctx, "alice", "secret", developer.Name, ""); err != nil {
		t.Fatal(err)
	}
	if err := service.GrantRoleToRole(ctx, reader.Name, developer.Name); err != nil {
		t.Fatal(err)
	}
	principal, err := service.Authenticate(ctx, "alice", "secret")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct{ name, requested, want string }{
		{name: "default", want: "DEVELOPER"},
		{name: "direct", requested: "developer", want: "DEVELOPER"},
		{name: "inherited", requested: "reader", want: "READER"},
		{name: "implicit public", requested: "public", want: "PUBLIC"},
	} {
		t.Run(test.name, func(t *testing.T) {
			role, resolveErr := service.ResolveActiveRole(ctx, principal.UserID, test.requested)
			if resolveErr != nil {
				t.Fatal(resolveErr)
			}
			if role.Name != test.want {
				t.Fatalf("got %s, want %s", role.Name, test.want)
			}
		})
	}
	if _, err := service.ResolveActiveRole(ctx, principal.UserID, RoleUserAdmin); !errors.Is(err, ErrRoleNotGranted) {
		t.Fatalf("expected ErrRoleNotGranted, got %v", err)
	}
}
