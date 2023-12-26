package main

import (
	billy "github.com/go-git/go-billy/v5"
	nfs "github.com/willscott/go-nfs"
	nfshelper "github.com/willscott/go-nfs/helpers"

	"flag"
	"fmt"
	"io"
	fs1 "io/fs"
	"log"
	"net"
	"os/signal"
	"syscall"
	"slices"
	"os"
	"time"
	"path"
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
	
	dirs:=fs.joinRelative(path)[fs.indexOfFirstExisting(path):]
	dirMap:=make(map[string]os.FileInfo)
	result:=make([]os.FileInfo,0)
	
	
	for i, dir :=range dirs{
		dirEntries,err:=os.ReadDir(dir)
		if err!=nil{
			if i==0{ //Since the top-level directory determines the behavior of everything else, if it isn't a directory, no point in continuing
				return make([]os.FileInfo,0), err
			}else{ //The higher-level directories cover for it
				continue
			}
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
				if !fs.checkIfExists(fs.Join(path,entry.Name())){
					continue
				}
				dirMap[entry.Name()]=info
			}
		}
	}
	
	for _,info := range dirMap{
		result=append(result,info)
	}
	
	err:=unspecifiedError
	err=nil
	fmt.Println("dirMap: ",dirMap)
	return result,err
}

func (fs OverlayFS) Chtimes(name string, atime time.Time, mtime time.Time) error{
	if !fs.checkIfExists(name){
		return os.ErrNotExist
	}
	
	fmt.Println("Chtimes:",name)
	
	if fs.getModeofFirstExisting(name)=="RW"{
		return os.Chtimes(fs.findFirstExisting(name),atime,mtime)
	}else{
		return nil //On RO, it's a no-op
	}
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
	isCreate:=(flag & os.O_CREATE !=0)
	original_filename:=filename
	
	fmt.Println("Openfile:",filename)
	
	if !fs.checkIfExists(filename){ //If file has been explicitly deleted, any call to Open will fail...
		if !isCreate{ //...except for CREATE
			return nil, os.ErrNotExist
		}
	}
	
	
	
	
	if (fs.checkIfExists(filename) && fs.getModeofFirstExisting(filename)=="RO" && isWrite){
		fs.deletedMap[filename]=true //Done to force createPath to create file above the current existing one
		COW:=false
		COWPath:=""
		originalPath:=fs.findFirstExisting(filename)
		create_path, err:= fs.createPath(filename)
		fmt.Println(create_path)
		delete(fs.deletedMap,filename)
		
		if err!=nil{
			return nil, err
		}else{	
			COW=true
			COWPath=create_path
		}
		
		fileStat,_:=os.Lstat(originalPath)
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
				dest.Close()
			}else if isSymlink{
				target,_ :=os.Readlink(originalPath)
				os.Symlink(target,COWPath)
			}
			
			setPermissions(originalPath, COWPath) //Make file with the same permissions as before
			
			filename=COWPath
		}else{
			return nil, os.ErrInvalid
		}
	}else{
	
		if isCreate{
			create_path, err:=fs.createPath(filename)
			if err!=nil{
				return nil, err
			}
			
			filename=create_path
		}else{
			filename=fs.findFirstExisting(filename)
		}
	}
	
	if isCreate{
		defer fs.createErrorCheck(filename, original_filename)
	}
	
	tempStat,err:=fs.Lstat(original_filename)
	readLink,_:=fs.Readlink(original_filename)
	if err==nil && (tempStat.Mode() & os.ModeSymlink !=0){
		fmt.Println("Symlink1: %s",original_filename)
		fmt.Println("Maps to: %s", readLink)
	}
	
	open,err:=os.OpenFile(filename,flag,perm)
	
	fmt.Println("Openfile finished!")
	return &OverlayFile{open},err
}

func (fs OverlayFS) Remove(filename string) error{
	if !fs.checkIfExists(filename){
		return os.ErrNotExist
	}
	
	fmt.Println("Remove:",filename)
	
	var err error
	if fs.getModeofFirstExisting(filename)=="RW"{
		err=os.Remove(fs.findFirstExisting(filename))
		if err!=nil{
			fmt.Println("Remove Error: ",err)
			return err
		}
	}else{
		fileStat,_:=fs.Lstat(filename)
		if fileStat.IsDir(){
			fileRead,_:=fs.ReadDir(filename)
			if len(fileRead)!=0{ //Can't remove a non-empty directory
				return unspecifiedError
			}
		}
	}
			
	
	fs.addToDeleted(filename)
	
	return nil
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
	cacheHelper := nfshelper.NewCachingHandler(handler, 100000000000000)
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
