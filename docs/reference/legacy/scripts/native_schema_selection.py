"""Build literal PostgreSQL 17 pg_dump schema selector arguments."""
import application_inventory


def pg_dump_schema_args(schemas):
    """Return --strict-names and literal PG17 --schema argv arguments."""
    names = application_inventory.validate_schemas(schemas)
    return ['--strict-names', *[
        '--schema="%s"' % name.replace('"', '""') for name in names
    ]]
