module overlay-nfs

go 1.21.2

//replace github.com/willscott/go-nfs => /home/system/go-nfs

//replace github.com/willscott/go-nfs => /home/system/test/go-nfs

replace github.com/willscott/go-nfs => github.com/DUOLabs333/go-nfs v0.0.2-0.20240101220624-124a04d74eb3

require (
	github.com/go-git/go-billy/v5 v5.5.0
	github.com/willscott/go-nfs v0.0.2-0.20231216210521-c4b888eab55f
	golang.org/x/sys v0.15.0
)

require (
	github.com/google/uuid v1.5.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/rasky/go-xdr v0.0.0-20170124162913-1a41d1a06c93 // indirect
	github.com/willscott/go-nfs-client v0.0.0-20200605172546-271fa9065b33 // indirect
)
