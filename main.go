package main
import (
	billy "github.com/go-git/go-billy/v5"
	nfs "github.com/willscott/go-nfs"
	nfshelper "github.com/willscott/go-nfs/helpers"
	
	"strings"
	"slices"
	"flag"
	"net"
)

type OverlayFS struct {
	paths []string
	modes []string
	mountpoint string
	
	billy.Filesystem
	
	billy.Change
}

func (fs OverlayFS) New(options []string, mountpoint string) (OverlayFS){
	result:=OverlayFS{}
	
	result.paths=make([]string,0)
	result.modes=make([]string,0)
	
	result.mountpoint=mountpoint
	for _, value := range options{
		split_index:=strings.LastIndex(value,"=")
		if ((split_index==-1) || !slices.Contains([]string{"RO","RW"},value[split_index+1:])){
			value+="=RO"
			split_index=strings.LastIndex(value,"=")
		}
		result.paths=append(result.paths,value[:split_index])
		result.modes=append(result.paths,value[split_index+1:])
	}
	
	return result
}

func runServer(args []string){
	listener, err := net.Listen("tcp", ":10000")
}
	
func main(){


}
