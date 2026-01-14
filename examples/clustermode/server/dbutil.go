// Copyright 2026 The A2A Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"database/sql"
	"errors"

	"github.com/a2aproject/a2a-go/log"
)

func closeSQLRows(ctx context.Context, rows *sql.Rows) {
	if err := rows.Close(); err != nil {
		log.Warn(ctx, "failed to close rows", err)
	}
}

func rollbackTx(ctx context.Context, tx *sql.Tx) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		log.Warn(ctx, "failed to rollback transaction", err)
	}
}
