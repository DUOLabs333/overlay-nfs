package main

import(

	billy "github.com/go-git/go-billy/v5"
	nfs "github.com/willscott/go-nfs"
	nfshelper "github.com/willscott/go-nfs/helpers"

	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"slices"
	"syscall"
	"time"
	"strconv"
	"encoding/json"
	"strings"
	"golang.org/x/sys/unix"
	"errors"
	"io"

)

func runCommand(args... string){
	command:=exec.Command(args[0],args[1:]...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Run()
}

func newEmpty[T any]() T{
	return *(new(T))
}

type OverlayFS struct {
	paths []string
	modes []string
	mountpoint string
	deletedMap map[string]bool
	deletedMapFile string
	
	billy.Filesystem
	
	billy.Change
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

func NewFS(options []string, mountpoint string) (OverlayFS){
	result:=OverlayFS{}
	
	result.paths=make([]string,0)
	result.modes=make([]string,0)
	result.mountpoint=mountpoint
	result.deletedMap=make(map[string]bool)
	
	mountpoint_Info,_:=os.Stat(mountpoint)
	mountpoint_Stat,_:=mountpoint_Info.Sys().(*syscall.Stat_t)
	result.deletedMapFile=path.Join(filepath.Dir(mountpoint),"overlay-nfs_"+strconv.FormatUint(mountpoint_Stat.Ino,10)+".json")
	
	_, err:= os.Stat(result.deletedMapFile)
	if err==nil{
		bytes, _ := os.ReadFile(result.deletedMapFile)
		json.Unmarshal(bytes,&(result.deletedMap))
	}
		
	
	for _, value := range options{
		split_index:=strings.LastIndex(value,"=")
		if ((split_index==-1) || !slices.Contains([]string{"RO","RW"},value[split_index+1:])){
			value+="=RO"
			split_index=strings.LastIndex(value,"=")
		}
		result.paths=append(result.paths,value[:split_index])
		result.modes=append(result.modes,value[split_index+1:])
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

func (fs OverlayFS) findFirstRW(filename string) string{
	for i,_ := range fs.paths{
		if fs.modes[i]=="RW"{ //Create in the first RW directory
			return path.Join(fs.paths[i],filename)
		}
	}
	return ""
}
func (fs OverlayFS) getModeofFirstExisting(filename string) string{
	 overlayfs_filename:=fs.findFirstExisting(filename)
	 for i,_:= range fs.paths{
		 if path.Join(fs.paths[i],filename)==overlayfs_filename{
			 return fs.modes[i]
			 break
		 }
	 }
	 return ""
}
func (fs OverlayFS) checkIfDeleted(filename string) bool{
	dirs:=strings.Split(filename,string(os.PathSeparator))

	for i, _ := range dirs{
		partialFilename:=path.Join(dirs[:i+1]...)
		_, exists := fs.deletedMap[partialFilename]
		if exists{
			fmt.Println("Deleted:",partialFilename)
			return true
		}
	}
	return false
		
}

func (fs OverlayFS) writeToDeletedMapFile(){
	bytes,_:=json.Marshal(fs.deletedMap)
	os.WriteFile(fs.deletedMapFile,bytes,0644)
}
func (fs OverlayFS) addToDeleted(filename string){
	fs.deletedMap[filename]=true
	fs.writeToDeletedMapFile()
}

func (fs OverlayFS) removefromDeleted (filename string){
	delete(fs.deletedMap,filename)
	fs.writeToDeletedMapFile()
}

func (fs OverlayFS) Join(elem ...string) string{
	fmt.Println("Join:",elem)
	return path.Join(elem...)
}

func (fs OverlayFS) ReadDir(path string) ([]os.FileInfo, error){
	
	if fs.checkIfDeleted(path){
		return make([]os.FileInfo,0), os.ErrNotExist
	}
	
	fmt.Println("ReadDir:",path)
	
	dirs:=fs.joinRelative(path)
	dirMap:=make(map[string]os.FileInfo)
	result:=make([]os.FileInfo,0)
	existsOneDir:=false //Whether at least one dir in dirs is a directory
	
	
	for _, dir :=range dirs{
		dirEntries,err:=os.ReadDir(dir)
		if err!=nil{
			continue
		}else{
			existsOneDir=true
		}
		for _, entry:= range dirEntries{
			_,ok:=dirMap[entry.Name()]
			if ok{ //If there's A/C and B/C, take A/C
				continue
			}else{
				info, err:=entry.Info()
				if err!=nil{
					continue
				}
				dirMap[entry.Name()]=info
			}
		}
	}
	
	for _,info := range dirMap{
		result=append(result,info)
	}
	
	err:=errors.New("H")
	err=nil
	if !existsOneDir{
		err=os.ErrInvalid
	}
	return result,err
}

func (fs OverlayFS) Stat(filename string) (os.FileInfo, error){
	if fs.checkIfDeleted(filename){
		return newEmpty[os.FileInfo](), os.ErrNotExist
	}
	
	fmt.Println("Stat:",filename)
	/*
	overlayfs_filename:=fs.findFirstExisting(filename)
	
	symlink, err:=fs.Readlink(overlayfs_filename)
	
	if err==nil{
		if !filepath.IsAbs(symlink){
			symlink=path.Join(filepath.Dir(filename),symlink)
		}
		filename=symlink
	}else{
		filename=filename
	}
	*/
	return fs.Lstat(filename)
}

func (fs OverlayFS) Lstat(filename string) (os.FileInfo, error){
	if fs.checkIfDeleted(filename){
		return newEmpty[os.FileInfo](), os.ErrNotExist
	}
	
	fmt.Println("Lstat:",filename)

	return os.Lstat(fs.findFirstExisting(filename))
}

func (fs OverlayFS) Readlink(link string) (string, error){
	 if fs.checkIfDeleted(link){
		 return "",os.ErrNotExist
	 }
	 
	 fmt.Println("Readlink:",link)
	 
	 return os.Readlink(fs.findFirstExisting(link))
}
	 
func (fs OverlayFS) Chtimes(name string, atime time.Time, mtime time.Time) error{
	if fs.checkIfDeleted(name){
		return os.ErrNotExist
	}
	
	fmt.Println("Chtimes:",name)
	return os.Chtimes(fs.findFirstExisting(name),atime,mtime)
}

func (fs OverlayFS) Open(filename string) (billy.File, error){
	fmt.Println("Open:",filename)
	return fs.OpenFile(filename,os.O_RDONLY,0)
}

func (fs OverlayFS) Create(filename string) (billy.File, error){
	fmt.Println("Create:",filename)
	return fs.OpenFile(filename,os.O_CREATE | os.O_TRUNC, 0666)
}

func setPermissions(src, dest string) {
	srcStat,_:=os.Stat(src)
	os.Chmod(dest,srcStat.Mode())
	
	srcUid:=-1
	srcGid:=-1
	
	if srcInfo, ok := srcStat.Sys().(*syscall.Stat_t); ok{
		srcUid=int(srcInfo.Uid)
		srcGid=int(srcInfo.Gid)
	}
	
	os.Chown(dest,srcUid,srcGid)
}

func (fs OverlayFS) OpenFile(filename string, flag int, perm os.FileMode) (billy.File, error){
	original_filename:=filename
	
	fmt.Println("Openfile:",original_filename)
	
	if fs.checkIfDeleted(original_filename){
		if (flag & os.O_CREATE == 0){ //Create could create file, so can't say it's not neccessary
			return newEmpty[billy.File](), os.ErrNotExist
		}
	}
			
	possible_filename:=fs.findFirstExisting(original_filename)
	_, err:=os.Stat(possible_filename)
	if !os.IsNotExist(err){ //If file exists, use it
		filename=possible_filename
	}else{
		if (flag & os.O_CREATE == os.O_CREATE){
			allRO:=true
			for i,_ := range fs.paths{
				fmt.Println(fs.modes[i])
				if fs.modes[i]=="RW"{ //Create in the first RW directory
					filename=path.Join(fs.paths[i],original_filename)
					allRO=false
					break
				}
			}
			if allRO{
				return *new(billy.File), os.ErrPermission
			}
		}
	}
	
	if fs.getModeofFirstExisting(original_filename)=="RO" && ((flag & os.O_RDWR==os.O_RDWR) || (flag & os.O_WRONLY==os.O_WRONLY)){ //Implement COW only when RDWR or WRONLY.
	
		COW:=false
		COWDir:=""
		originalDir:=""
		
		for i,dir := range fs.paths{
			if fs.modes[i]!="RW"{
				continue
			}
			
			if fs.Join(dir,original_filename)==filename{ //Stop when you reach the current RO directory
				originalDir=dir
				break
			}
			
			if fs.modes[i]=="RW"{ //Get the highest available RW directory that's above the current RO directory and COW there (the highest available is just a choice --- we could have also chosen the RW directory that is closest to the current RO directory that is still higher than it)
				if COW==false{
					COWDir=dir
					COW=true
				}
			}		
		}
		
		fileStat,_:=os.Stat(filename)
		if COW {
			COW=fileStat.Mode().IsRegular() //Check that file is regular
		}
		
		if COW {
			parent_dirs:=make([]string,0)
			curr:=filepath.Dir(original_filename)
			
			for true{
				if curr=="."{
					break
				}
				
				parent_dirs=append(parent_dirs,curr)
				curr=filepath.Dir(curr)
			}
			
			slices.Reverse(parent_dirs)
			
			for _,dir := range parent_dirs{ //Create parent directories of file in COW directories
				newDir:=fs.Join(COWDir,dir)
				os.Mkdir(newDir,0700)
				setPermissions(fs.Join(originalDir,dir),newDir)
			}
			
			src,_:=os.Open(filename)
			
			newFilename:=fs.Join(COWDir,original_filename)
			dest,_:=os.Create(newFilename)
			
			io.Copy(dest,src)
			setPermissions(filename, newFilename) //Make file with the same permissions as before
			
			filename=newFilename
		}else{
			flag=flag & ^(os.O_RDONLY | os.O_RDWR | os.O_WRONLY)
			flag |= os.O_RDONLY
		}
	}
		
		
	open,err:=os.OpenFile(filename,flag,perm)
	
	if (flag & os.O_CREATE == os.O_CREATE){
		_,create_err:=os.Stat(filename)
		if create_err==nil{ //If Create was successful, it is no longer deleted
			fs.removefromDeleted(original_filename)
		}
	}
	
	return &OverlayFile{open},err
}

func (fs OverlayFS) Remove(filename string) error{
	if fs.checkIfDeleted(filename){
		return os.ErrNotExist
	}
	
	fmt.Println("Remove:",filename)
	
	original_filename:=filename
	
	filename=fs.findFirstExisting(original_filename)
	
	var err error
	if fs.getModeofFirstExisting(original_filename)=="RW"{
		err=os.Remove(filename)
	}else{
		err=nil
	}
	
	fs.addToDeleted(original_filename)
	
	return err
}

func (fs OverlayFS) Mknod(path string, mode uint32, major uint32, minor uint32) error {
	if fs.checkIfDeleted(path){
		return os.ErrNotExist
	}
	
	filename:=fs.findFirstRW(path)
	if filename==""{
		return os.ErrPermission
	}
	
	dev := unix.Mkdev(major, minor)
	return unix.Mknod(filename, mode, int(dev))
}

func (fs OverlayFS) Mkfifo(path string, mode uint32) error {
	if fs.checkIfDeleted(path){
		return os.ErrNotExist
	}
	
	filename:=fs.findFirstRW(path)
	if filename==""{
		return os.ErrPermission
	}
	
	return unix.Mkfifo(filename, mode)
}

func (fs OverlayFS) Link(link string, path string) error {
	if fs.checkIfDeleted(path){
		return os.ErrNotExist
	}
	
	filename:=fs.findFirstRW(path)
	if filename==""{
		return os.ErrPermission
	}
	
	return unix.Link(fs.findFirstExisting(link), filename)
}

func (fs OverlayFS) Socket(path string) error {
	if fs.checkIfDeleted(path){
		return os.ErrNotExist
	}
	
	filename:=fs.findFirstRW(path)
	if filename==""{
		return os.ErrPermission
	}
	
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return err
	}
	return unix.Bind(fd, &unix.SockaddrUnix{Name: filename})
}

var port chan int = make(chan int,1)

func runServer(options []string,mountpoint string){
	listener, err := net.Listen("tcp", ":0") //Get random unused port
	
	listenerAddr:=listener.Addr()
	addr,_:=net.ResolveTCPAddr(listenerAddr.Network(),listenerAddr.String())
	
	port <- addr.Port
	
	panicOnErr(err, "starting TCP listener")
	fs:=NewFS(options,mountpoint)
	handler := nfshelper.NewNullAuthHandler(fs)
	cacheHelper := nfshelper.NewCachingHandler(handler, 100000000000)
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
go runServer(options,mountpoint)

serverPort:= <- port
runCommand("sudo","mount", "-t", "nfs", fmt.Sprintf("-oport=%[1]d,mountport=%[1]d,vers=3,tcp,noacl,nolock",serverPort),"-vvv","127.0.0.1:/", mountpoint)

<-done //wait until SIGTERM or SIGINT, then unmount

runCommand("sudo","umount", "-l",mountpoint)
fmt.Println()
}
