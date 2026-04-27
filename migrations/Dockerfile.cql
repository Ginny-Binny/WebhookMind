# One-shot CQL migrator: applies every .cql file in /migrations against ScyllaDB.
# Used by the compose `migrate-scylla` service.

FROM scylladb/scylla:5.4

# Wait helper + apply loop. cqlsh ships with the scylla image.
ENTRYPOINT ["sh", "-c", "for f in /migrations/*.cql; do echo \"applying $f\"; cqlsh $SCYLLA_HOST -f \"$f\" || exit 1; done; echo migrations complete"]
