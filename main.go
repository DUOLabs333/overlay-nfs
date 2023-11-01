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
	
	//If there's A/C and B/C, take A/C
	for _, dir :=range dirs{
		dirEntries,err:=os.ReadDir(dir)
		if err!=nil{
			continue
		}
		for _, entry:= range dirEntries{
			_,ok:=dirMap[entry.Name()]
			if ok{
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
	
	fmt.Println(result)
	return result,nil
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
	return fs.OpenFile(filename,os.O_CREATE | os.O_TRUNC,0666)
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
	if !os.IsNotExist(err){
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
	
	if fs.getModeofFirstExisting(original_filename)=="RO"{
		flag=flag & ^(os.O_RDONLY | os.O_RDWR | os.O_WRONLY)
		flag |= os.O_RDONLY
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

//If remove, remove if first file found is in RW. Regardless add to removed
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

func runServer(options []string,mountpoint string){
	listener, err := net.Listen("tcp", ":10000") //Later, use port that's defined in main as argument to function
	panicOnErr(err, "starting TCP listener")
	fs:=NewFS(options,mountpoint)
	handler := nfshelper.NewNullAuthHandler(fs)
	cacheHelper := nfshelper.NewCachingHandler(handler, 100000)
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

runCommand("sudo","mount", "-t", "nfs", "-oport=10000,mountport=10000,vers=3,tcp,noacl","-vvv","127.0.0.1:/", mountpoint)


<-done

//wait until SIGTERM or SIGINT, then unmount
runCommand("sudo","umount", "-l",mountpoint)
fmt.Println()
}
