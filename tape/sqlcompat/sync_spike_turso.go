//go:build turso_sync_eval

package sqlcompat

import (
	"context"
	"os"

	tursosync "turso.tech/database/tursogo"
)

func newTursoSyncDB(ctx context.Context, localPath, remoteURL, token string, bootstrapIfEmpty bool) (*tursosync.TursoSyncDb, error) {
	return tursosync.NewTursoSyncDb(ctx, tursosync.TursoSyncDbConfig{
		Path:             localPath,
		RemoteUrl:         remoteURL,
		AuthToken:        token,
		BootstrapIfEmpty: bootstrapIfEmpty,
	})
}
