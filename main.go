package main
import (
	billy "github.com/go-git/go-billy/v5"
	nfs "github.com/willscott/go-nfs"
	nfshelper "github.com/willscott/go-nfs/helpers"
	
	"strings"
	"slices"
	"flag"
	"net"
	"os/exec"
	"os"
	"log"
	"time"
	"path"
)

type OverlayFS struct {
	paths []string
	modes []string
	mountpoint string
	
	billy.Filesystem
	
	billy.Change
}

func NewFS(options []string, mountpoint string) (OverlayFS){
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

func (fs OverlayFS) joinRelative(Path string) ([]string){
	result:=make([]string,0,len(fs.paths))
	for i, _ := range fs.paths{
		result=append(result,path.Join(fs.paths[i],Path))
	}
	return result
}
func (fs OverlayFS) ReadDir(path string) ([]os.FileInfo, error){
dirs:=fs.joinRelative(path)
result:=make([]os.FileInfo,0)
for _, dir :=range dirs{
	dirEntries,_:=os.ReadDir(dir)
	for _, entry:= range dirEntries{
		info,_:=entry.Info()
		result=append(result,info)
	}
}
return result,nil
}

func (fs OverlayFS) Stat(filename string) (os.FileInfo, error){
files:=fs.joinRelative(filename)
file, _:=os.Open(files[0])
return file.Stat()
}

func (fs OverlayFS) Join(elem ...string) string{
	return path.Join(elem...)
}
func runServer(options []string,mountpoint string){
	listener, err := net.Listen("tcp", ":10000") //Later, use port that's defined in main as argument to function
	panicOnErr(err, "starting TCP listener")
	fs:=NewFS(options,mountpoint)
	handler := nfshelper.NewNullAuthHandler(fs)
	cacheHelper := nfshelper.NewCachingHandler(handler, 1)
	panicOnErr(nfs.Serve(listener, cacheHelper), "serving nfs")
}

func panicOnErr(err error, desc ...interface{}) {
	if err == nil {
		return
	}
	log.Println(desc...)
	log.Panicln(err)
}

func main(){
flag.Parse()
args:=flag.Args()

options:=args[:len(args)-1]
mountpoint:=args[len(args)-1]
go runServer(options,mountpoint);

for {
	conn, _ := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", "10000"), 1*time.Millisecond)
	if conn != nil {
		conn.Close()
		break
	}
}

command:=exec.Command("sudo","mount", "-t", "nfs", "-oport=10000,mountport=10000,vers=3,tcp","-v","127.0.0.1:/", mountpoint)
command.Stdout = os.Stdout
command.Stderr = os.Stderr
command.Run()
}
