package database

import "errors"

const maxSchemaSelections = 256

var ErrInvalidSchemaSelection = errors.New("invalid schema selection")

func validateSchemaNames(names []string) error {
	if len(names) > maxSchemaSelections {
		return ErrInvalidSchemaSelection
	}
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if !validIdentifier(name) {
			return ErrInvalidSchemaSelection
		}
		if _, ok := seen[name]; ok {
			return ErrInvalidSchemaSelection
		}
		seen[name] = struct{}{}
	}
	return nil
}
