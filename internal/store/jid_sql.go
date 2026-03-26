package store

import "fmt"

func canonicalChatJIDExpr(expr string) string {
	return fmt.Sprintf(
		`CASE
			WHEN split_part(%[1]s, '@', 2) = 'lid' THEN COALESCE(
				(SELECT lm.pn || '@s.whatsapp.net' FROM whatsmeow_lid_map lm WHERE lm.lid = split_part(%[1]s, '@', 1)),
				%[1]s
			)
			ELSE %[1]s
		END`,
		expr,
	)
}
