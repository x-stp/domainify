#!/bin/sh
################################################
##           D0M41N1FY v1.x.x                ##
##  ----------------------------------------  ##
##  IF YOU ALTER ANY PART OF THIS TOOL YOU    ##
## WILL BE OWNED, RM'D, AND PUT IN NEXT ISSUE ##
## ------------------------------------------ ##
##  IF YOU ALTER ANY PART OF THIS TOOL YOU    ##
## WILL BE OWNED, RM'D, AND PUT IN NEXT ISSUE ##
## ------------------------------------------ ##
##  IF YOU ALTER ANY PART OF THIS TOOL YOU    ##
## WILL BE OWNED, RM'D, AND PUT IN NEXT ISSUE ##
## ------------------------------------------ ##
##           fast domain extraction           ##
################################################
##::::::::::::::::::::::::::::::::::::::::::::##
##:'####::::::'########:'##::::::::'#######:::##
##'##  ##:'##: ##.....:: ##:::::::'##.... ##::##
##..::. ####:: ##::::::: ##::::::: ##:::: ##::##
##:::::....::: ######::: ##:::::::: #######:::##
##:::::::::::: ##...:::: ##:::::::'##.... ##::##
##:::::::::::: ##::::::: ##::::::: ##:::: ##::##
##:::~d0m[1]:: ########: ########:. #######:::##
##::::::::::::........::........:::.......::::##
################################################
## the definitive src for domain extraction   ##
################################################
## do "go build -o domainify main.go" 2 build ##
## usage: ./domainify input.txt               ##
## or: cat domains.txt | ./domainify          ##
##  <*> extracts clean domains from URLs      ##
##  <*> handles hostnames with ports          ##
##  <*> concurrent processing with workers    ##
##  <*> live metrics display (-live flag)     ##
##  <*> prometheus endpoint support           ##
##  <*> graceful shutdown handling            ##
##  <*> deduplication of output domains       ##
##  <*> supports pipe input for r33l h4x0rz   ##
################################################
## where have all the clean d0m41ns g0ne!!!   ##
################################################

cat <<'-+-+'> /dev/null
[BOI]

    .~#:._.:#~#:._.:#~#:._.:#~#:._.:#~#:._.:#~#:._.:#~#:._.:#~#:._.:#~.
    |#$%$#@%!$@^%@$^!@#@#%!@#$^@!$#^%!@$#$%@!#$%^!@$^%#$^!@$%@#@^$#!@#|
    |#:::::::::::::::::::::::::::::::::::::::::::::::::::::::::::::::#|
    |#::'####::::::'########:'##::::::::'#######::'##:'#######:'##:::#|
    |#:'##  ##:'##: ##.....:: ##:::::::'##.... ##: #::...... #:: #:::#|
    |#:..::. ####:: ##::::::: ##::::::: ##:::: ##: #:::::::: #:: #:::#|
    |#::::::....::: ######::: ##:::::::: #######:: #::: ######:: #:::#|
    |#::::::::::::: ##...:::: ##:::::::'##.... ##: #:::..... #:: #:::#|
    |#::::::::::::: ##::::::: ##::::::: ##:::: ##: #:::::::: #:: #:::#|
    |#::::::::::::: ########: ########:. #######:: ##: #######: ##:::#|
    |#:::::::::::::........::........:::.......:::..::.......::..::::#|
    |#:::::::::::::::::::::::::::::::::::::::::::::::::::::::::::::::#|
    |#@#$!@%$^%@!$#%$@%^#!^$#@^%!@%#%!@#^$%@!^$#$^!@$^#$^^%@%@#!@#!@$#|
    |#:::::::::::::EXTRACTING D0M41NS SINCE 2025::::::::::::::::::::::#|
    |#@#$!@%$^%@!$#%$@%^#!^$#@^%!@%#%!@#^$%@!^$#$^!@$^#$^^%@%@#!@#!@$#|
    `~#:._.:#~#:._.:#~#:._.:#~#:._.:#~#:._.:#~#:._.:#~#:._.:#~#:._.:#~'

.----------------------------------------------------------------.
; PERFORMANC3 M3TR1CS (tested on production h4rdw4re)            ;
`----------------------------------------------------------------'
; 10 domains              6ms             1.67k/sec             ;
; 1k domains              8ms             125k/sec              ; 
; 100k domains            144ms           694k/sec              ;
.----------------------------------------------------------------.

.----------------------------------------------------------------.
; BENCHMARK D4T4 (go test -bench=. -benchmem)                   ;
`----------------------------------------------------------------'
; BenchmarkNormalizeInput         847 ns/op    352 B/op   4 alo ;
; BenchmarkProcessingPipeline     311µs/op   57KB/op   767 alo  ;
;                                                               ;
; PlainDomain                      84 ns/op     32 B/op   1 a   ;  
; HTTPSUrl                        339 ns/op    144 B/op   1 a   ;
; HostWithPort                     31 ns/op      0 B/op   0 a   ;
; FTPUrl                          343 ns/op    144 B/op   1 a   ;
; ComplexUrl                      506 ns/op    144 B/op   1 a   ;
`----------------------------------------------------------------'

.----------------------------------------------------------------.
; US4G3 3X4MPL3Z                                                ;
`----------------------------------------------------------------'
; ./domainify input.txt           # basic extraction            ;
; ./domainify -live input.txt     # with live m3tr1cs          ;  
; ./domainify -w 16 input.txt     # 16 w0rk3rz                 ;
; cat domains.txt | ./domainify   # p1p3 1nput                 ;
; ./domainify -d "," input.txt    # comma s3par4t3d            ;
; ./domainify -m-addr :9090       # prom3th3us 3ndp01nt        ;
`----------------------------------------------------------------'

.----------------------------------------------------------------.
; 1NPUT F0RM4TZ SUPP0RT3D                                       ;
`----------------------------------------------------------------'
; example.com                     # plain d0m41ns               ;
; https://api.example.com/path    # HTTP/HTTPS URLz             ;
; database.service.com:5432       # h0stn4m3z w1th p0rtz       ;
; ftp://files.example.org:21/up   # FTP URLz                    ;
;                                                               ;
; All g3t n0rm4l1z3d t0 cl34n d0m41n f0rm4t.                   ;
`----------------------------------------------------------------'

.----------------------------------------------------------------.
; F34TUR3Z                                                      ;
`----------------------------------------------------------------'
; [x] Domain extraction from mixed URL/hostname formats         ;
; [x] Concurrent processing with configurable worker pools      ;
; [x] Live statistics display during processing                 ;
; [x] Prometheus metrics endpoint support (/metrics)            ;
; [x] Graceful shutdown handling with context cancellation      ;
; [x] Memory-efficient processing with sync.Pool buffers        ;
; [x] Deduplication of output domains                           ;
; [x] Pipe input support for automation                         ;
`----------------------------------------------------------------'

.----------------------------------------------------------------.
; BU1LD & T3ST                                                  ;
`----------------------------------------------------------------'
; go build -o domainify main.go                                 ;
; go test -bench=. -benchmem      # run p3rf0rm4nc3 b3nchm4rkz  ;
; go test -bench=. -cpu=1,2,4,8   # t3st scal1ng 4cr0ss CPUz   ;
;                                                               ;
; # g3n3r4t3 t3st d4t4:                                         ;
; for i in {1..1000}; do echo "test${i}.com"; done > test.txt   ;
`----------------------------------------------------------------'

.----------------------------------------------------------------.
; 4RCH1T3CTUR3 N0T3Z                                            ;
`----------------------------------------------------------------'
; Pipeline: Input -> Workers -> Deduplication -> Output         ;
; Workers: Configurable goroutine pool with atomic counters     ;
; Memory: sync.Pool for buffer reuse, pre-allocated channels    ;
; Parsing: golang.org/x/net/publicsuffix for domain validation  ;
; HTTP: Standard library client for basic operations            ;
; Metrics: Prometheus counters for input/output tracking        ;
;                                                               ;
; Dependencies: publicsuffix, prometheus client                 ;
; Platform: Cross-platform (Linux/macOS/Windows)               ;
; Go version: 1.24+                                             ;
`----------------------------------------------------------------'

.----------------------------------------------------------------.
; ~d0m[1] t34m m3mbr5                                           ;
`----------------------------------------------------------------'
; G0PH3R G0D        -> TH3 C0NC0RR3NCY M45T3R                   ;
; P1P3L1N3 P1MP     -> W0RK3R P00L WH15P3R3R                    ;
; BUFF3R B055       -> M3M0RY P00L M4G1C14N                     ;
; 5TR1NG 5L1C3R     -> D0M41N 3XTR4CT0R 3XTR40RD1N41R3          ;
; M3TR1C M4N14C     -> PR0M3TH3U5 M45T3R                        ;
; C0NT3XT C4NC3LL3R -> GR4C3FUL 5HUTD0WN 53N531                 ;
`----------------------------------------------------------------'

                              [EOF] domainify
                       "extract d0m41ns or GTFO" -~d0m[1]