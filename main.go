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
	"os/signal"
	"path"
	"syscall"
	"time"
	"errors"
	"io"
	"slices"
	fs1 "io/fs"

)


func (fs OverlayFS) Join(elem ...string) string{
	fmt.Println("Join:",elem)
	return path.Join(elem...)
}

func (fs OverlayFS) ReadDir(path string) ([]os.FileInfo, error){
	
	if !fs.checkIfExists(path){
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

func (fs OverlayFS) Chtimes(name string, atime time.Time, mtime time.Time) error{
	if !fs.checkIfExists(name){
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

func (fs OverlayFS) OpenFile(filename string, flag int, perm os.FileMode) (billy.File, error){
	writeConstants:=os.O_RDWR|os.O_WRONLY|os.O_APPEND|os.O_TRUNC
	
	isWrite:=(flag & writeConstants !=0)
	
	fmt.Println("Openfile:",filename)
	
	if !fs.checkIfExists(filename){ //If file has been explicitly deleted, any call to Open will fail...
		if (flag & os.O_CREATE == 0){ //...except for CREATE
			return newEmpty[billy.File](), os.ErrNotExist
		}
	}
	
	
	
	
	if (fs.checkIfExists(filename) && fs.getModeofFirstExisting(filename)=="RO" && isWrite){
		fs.deletedMap[filename]=true //Done to force createPath to create file above the current existing one
		COW:=false
		COWPath:=""
		originalPath:=fs.findFirstExisting(filename)
		create_path, err:= fs.createPath(filename)
		
		delete(fs.deletedMap,filename)
		
		if err!=nil{
			return newEmpty[billy.File](), err
		}else{	
			COW=true
			COWPath=create_path
		}
		
		fileStat,_:=os.Stat(originalPath)
		fileMode:=fileStat.Mode()
		isRegular := fileMode.IsRegular() 
		isSymlink := (fileMode & fs1.ModeSymlink != 0)
		
		COW=COW && (isRegular || isSymlink) //Check that file is regular or is a symlink
		
		if COW {
			num_parent_dirs:=len(parentDirs(filename))
			originalParentDirs:=parentDirs(originalPath)
			COWParentDirs:=parentDirs(COWPath)
			
			slices.Reverse(originalParentDirs) //From innermost to outermost
			slices.Reverse(COWParentDirs)
			
			for i := 0; i < num_parent_dirs; i++{ //Create parent directories of file in COW directories. From innermost to outermost
				originalDir:=originalParentDirs[i]
				COWDir:=COWParentDirs[i]
				
				fmt.Println(COWDir)
				fmt.Println(originalDir)
				
				os.MkdirAll(COWDir,0700)
				setPermissions(originalDir,COWDir)
			}
			
			src,_:=os.Open(originalPath)
			
			if isRegular{
				dest,_:=os.Create(COWPath)
			
				io.Copy(dest,src)
			}else if isSymlink{
				target,_ :=os.Readlink(originalPath)
				os.Symlink(target,COWPath)
			}
			
			setPermissions(originalPath, COWPath) //Make file with the same permissions as before
			
			filename=COWPath
		}else{
			return newEmpty[billy.File](), os.ErrInvalid
		}
	}else{
	
		if (flag & os.O_CREATE !=0){
			create_path, err:=fs.createPath(filename)
			if err!=nil{
				return newEmpty[billy.File](), err
			}
			
			filename=create_path
		}else{
			filename=fs.findFirstExisting(filename)
		}
	}
	
	fmt.Println(filename)
	open,err:=os.OpenFile(filename,flag,perm)
	
	if (flag & os.O_CREATE != 0){
		defer fs.createErrorCheck(filename)
	}
	
	return &OverlayFile{open},err
}

func (fs OverlayFS) Remove(filename string) error{
	if !fs.checkIfExists(filename){
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

func umount (mountpoint string) {
	runCommand("sudo","umount", "-l",mountpoint)
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

umount(mountpoint)
defer func (){umount(mountpoint); fmt.Println()}() //Unmount, even when panicking

go runServer(options,mountpoint)

serverPort:= <- port
runCommand("sudo","mount", "-t", "nfs", fmt.Sprintf("-oport=%[1]d,mountport=%[1]d,vers=3,tcp,noacl,nolock,soft",serverPort),"-vvv","127.0.0.1:/", mountpoint)

<-done //wait until SIGTERM or SIGINT
}
