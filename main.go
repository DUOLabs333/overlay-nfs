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
	"fmt"
	"os/signal"
	"syscall"
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

func (fs OverlayFS) findFirstExisting(Path string) string{
possibleFiles:=fs.joinRelative(Path)
for _, file := range possibleFiles{
_,err:=os.Stat(file);
if err==nil{
return file
}
}
return possibleFiles[len(possibleFiles)-1]
}

func (fs OverlayFS) ReadDir(path string) ([]os.FileInfo, error){
fmt.Println("Dir: "+path)
dirs:=fs.joinRelative(path)
result:=make([]os.FileInfo,0)
for _, dir :=range dirs{
	dirEntries,_:=os.ReadDir(dir)
	for _, entry:= range dirEntries{
		info,_:=entry.Info()
		result=append(result,info)
	}
}
fmt.Println("Files:",result)
return result,nil
}

func (fs OverlayFS) Stat(filename string) (os.FileInfo, error){
fmt.Println("Stat:",filename)
file, _:=os.Open(fs.findFirstExisting(filename))
return file.Stat()
}

//make function that finds the file in the first directory where it exists, and return it
//For deleting, delete from the first RW, and mask it (pretend like it doesn't exist)
//For creating, just make it in the first RW directory
func (fs OverlayFS) Lstat(filename string) (os.FileInfo, error){
fmt.Println("Lstat:",filename)
return os.Lstat(fs.findFirstExisting(filename))
}

type OverlayFile struct{
	*os.File
}

func (*OverlayFile) Unlock() error{
	return nil
}

func (*OverlayFile) Lock() error{
	return nil
}

func (fs OverlayFS) Open(filename string) (billy.File, error){
fmt.Println("Open:",filename)
open,err:=os.Open(fs.findFirstExisting(filename))
return &OverlayFile{open},err
}

func (fs OverlayFS) OpenFile(filename string, flag int, perm os.FileMode) (billy.File, error){
fmt.Println("Openfile:",filename)
fmt.Println(filename)
open,err:=os.OpenFile(fs.findFirstExisting(filename),flag,perm)
return &OverlayFile{open},err
}

func (fs OverlayFS) Join(elem ...string) string{
	fmt.Println("Join:",elem)
	return path.Join(elem...)
}
func runServer(options []string,mountpoint string){
	listener, err := net.Listen("tcp", ":10000") //Later, use port that's defined in main as argument to function
	panicOnErr(err, "starting TCP listener")
	fs:=NewFS(options,mountpoint)
	handler := nfshelper.NewNullAuthHandler(fs)
	cacheHelper := nfshelper.NewCachingHandler(handler, 1024)
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
sigs := make(chan os.Signal, 1)
signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
done := make(chan bool, 1)
go func() {
	<-sigs
	done <- true
	}()
	
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

mount_command:=exec.Command("sudo","mount", "-t", "nfs", "-oport=10000,mountport=10000,vers=3,tcp,noacl","-vvv","127.0.0.1:/", mountpoint)
mount_command.Stdout = os.Stdout
mount_command.Stderr = os.Stderr
mount_command.Run()
<-done

unmount_command:=exec.Command("sudo","umount", mountpoint)
unmount_command.Stdout = os.Stdout
unmount_command.Stderr = os.Stderr
unmount_command.Run()
}
//wait until SIGTERM or SIGINT, then unmount
