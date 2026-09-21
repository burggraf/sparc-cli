"""Offline tests for literal pg_dump schema selector construction."""
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import application_inventory
import native_schema_selection as selection


class NativeSchemaSelectionTest(unittest.TestCase):
    def test_returns_sorted_strict_literal_pg17_patterns(self):
        schemas = ['star*', 'quote"and\\slash', 'dot.name', '雪']
        self.assertEqual(
            selection.pg_dump_schema_args(schemas),
            ['--strict-names', '--schema="dot.name"', '--schema="quote""and\\slash"',
             '--schema="star*"', '--schema="雪"'],
        )

    def test_invalid_selection_fails_before_any_arguments_are_returned(self):
        for schemas in ([], ['same', 'same'], ['auth'], ['bad\x00name'], ['雪' * 22]):
            with self.subTest(schemas=repr(schemas)):
                with self.assertRaises(application_inventory.InspectionError):
                    selection.pg_dump_schema_args(schemas)

    def test_reuses_application_inventory_validator(self):
        with patch.object(selection.application_inventory, 'validate_schemas',
                          side_effect=application_inventory.InspectionError('invalid schema selection')) as validate:
            with self.assertRaises(application_inventory.InspectionError):
                selection.pg_dump_schema_args(['not-used'])
        validate.assert_called_once_with(['not-used'])


if __name__ == '__main__':
    unittest.main()
