set -e  # exit on error
shopt -s expand_aliases

# sling

# export _DEBUG=LOW
# export _DEBUG_CALLER_LEVEL=2
cd cmd/sling
go test -parallel 4 -run TestSuite

cd -

## test cli commands
go build -o sling-linux cmd/sling/*.go && chmod +x sling-linux
./sling-linux --version
alias sling="./sling-linux"

# should return menu, not error
sling run

sling run --src-stream file://core/dbio/filesys/test/test.excel2.xlsx --tgt-object file://test.xlsx

cat cmd/sling/tests/files/test1.1.csv | sling run --tgt-conn POSTGRES --tgt-object public.my_table --mode full-refresh
sling run --src-stream file://cmd/sling/tests/files/test1.1.csv --tgt-conn POSTGRES --tgt-object public.my_table --mode full-refresh
sling run --src-stream file://cmd/sling/tests/files/test1.1.csv --tgt-conn MSSQL --tgt-object dbo.my_table --mode full-refresh --tgt-options 'use_bulk: false'

cat cmd/sling/tests/files/test1.1.csv.gz | sling run --tgt-conn POSTGRES --tgt-object public.my_table --mode full-refresh
sling run --src-stream 'file://cmd/sling/tests/files/test1.1.csv.gz' --tgt-conn MYSQL --tgt-object mysql.my_table --mode full-refresh --tgt-options 'use_bulk: false'

cat cmd/sling/tests/files/test3.json | sling run --src-options "flatten: true" --tgt-conn POSTGRES --tgt-object public.my_table1 --tgt-options 'use_bulk: false' --mode full-refresh
sling run --src-stream 'file://cmd/sling/tests/files/test3.json'  --src-options "flatten: true" --tgt-conn POSTGRES --tgt-object public.my_table1 --tgt-options 'use_bulk: false' --mode full-refresh

# test various cli commands / flags
sling run --src-conn POSTGRES --src-stream public.my_table --stdout > /tmp/my_table.csv
sling run --src-conn POSTGRES --src-stream public.my_table --tgt-object file:///tmp/my_table.csv
sling run --src-conn POSTGRES --src-stream public.my_table --stdout --select 'id' -l 2
sling run --src-conn POSTGRES --src-stream public.my_table --stdout --select '-id' -l 2

# test binary
sling run --src-stream file://cmd/sling/tests/files/binary/test.bytes.csv --tgt-conn postgres --tgt-object public.my_table_bytes
SLING_ROW_CNT=1 sling conns exec postgres "select 1 from "postgres"."public"."my_table_bytes" where byte_val::bytea::text like '%89504e470d0a1a0a0000000d%'"

sling conns test POSTGRES
sling conns exec POSTGRES 'select count(1) from public.my_table'
sling conns discover POSTGRES
sling conns discover POSTGRES -s 'public.*'
sling conns discover local
sling conns discover prometheus --columns > /dev/null
sling conns discover prometheus --columns > /dev/null
sling run --src-conn prometheus --src-stream 'sum(go_gc_duration_seconds) by (job, instance, quantile) # {"start": "now-2M"}' --stdout  -d

sling run -r cmd/sling/tests/replications/r.05.yaml
sling run -r cmd/sling/tests/replications/r.05.yaml --streams 's3://ocral/mlo.community.test/channels.json,s3://ocral/mlo.community.test/random/'

SLING_STREAM_CNT=3 sling run -r cmd/sling/tests/replications/r.06.yaml
SLING_STREAM_CNT=11 sling run -r cmd/sling/tests/replications/r.07.yaml
SLING_STREAM_CNT=3 sling run -r cmd/sling/tests/replications/r.08.yaml
SLING_STREAM_CNT=">0" sling run -r cmd/sling/tests/replications/r.09.yaml
YEAR=2005 sling run -r cmd/sling/tests/replications/r.11.yaml
sling run -r cmd/sling/tests/replications/r.12.yaml

sling run -c cmd/sling/tests/task.yaml

sling run --src-stream 'file://cmd/sling/tests/files/parquet' --stdout > /dev/null

echo '' | sling run --stdout