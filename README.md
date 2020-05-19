# RAFT-KV-Store

## Build Protocol Buffer
```
make proto
```

## Build Program
```
make build
```

## Start Server
```
bin/kv -i node-0
bin/kv -i node-1 -l :11001 -r :12001 -j :11000
bin/kv -i node-2 -l :11002 -r :12002 -j :11000
```

## Start Client
```
bin/client -e :11000
```
Client commands:
- `get [key]`: get value from RAFT KV store
  - Examples: `get class` or `get "distributed system"`
- `put [key] [value]`: put (key, value) on RAFT KV store
  - Examples: `put class cs244b` or `put "2020 spring class" "distributed system"`
- `del [key]`: delete key from RAFT KV store
  - Examples: `del class` or `del "distributed system"`
- `txn`: start a transaction (Only `set` and `del` are supported in transaction)
- `endtxn`: end a transaction
  - Example:
   ```bazaar
   txn 
   put class cs244b
   put univ stanford
   del class
   end
   ```
- `exit`: exit client from server

## Leader:
```
curl localhost:11000/leader
```

## Put
```
curl -v localhost:11001/key -d '{"class-3": "cs244b5"}'
```

## Get:
```
curl -v localhost:11002/key/class-3
```

## Transactions:
```
curl -vvv localhost:11001/transaction -d '{"commands": [{"Command": "set", "Key": "name", "Value": "John"},{"Command": "set", "Key": "timezone", "Value": "pst"}]}'
```

## Get:
```
curl -vvv localhost:11002/key/timezone
```
