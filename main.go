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
	"golang.org/x/sys/unix"
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
	
	//NFS makes no distinction between Lstat and Stat so the following code had to be commented out.
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

	result, err:=os.Lstat(fs.findFirstExisting(filename))
	return OverlayStat{result,result,filename,fs}, err
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

func (fs OverlayFS) OpenFile(filename string, flag int, perm os.FileMode) (billy.File, error){
	original_filename:=filename
	
	fmt.Println("Openfile:",original_filename)
	
	if fs.checkIfDeleted(original_filename){ //If file has been explicitly deleted, any call to Open will fail...
		if (flag & os.O_CREATE == 0){ //...except for CREATE
			return newEmpty[billy.File](), os.ErrNotExist
		}
	}
	
	filename=fs.findFirstExisting(filename)

	if (flag & os.O_CREATE !=0){
		create_path, err:=fs.createPath(original_filename)
		if err!=nil{
			return newEmpty[billy.File](), err
		}
		
		filename=create_path
	}else{
	if fs.getModeofFirstExisting(original_filename)=="RO"{ //COW only matters in RO directories
	
	if ((flag & os.O_RDWR !=0) || (flag & os.O_WRONLY !=0)){ //Implement COW only when RDWR or WRONLY.
	
		fs.deletedMap[original_filename]=true //Done to force createPath to create file above the current existing one
		COW:=false
		COWPath:=""
		
		create_path, err:= fs.createPath(original_filename)
		
		delete(fs.deletedMap,original_filename)
		
		if err!=nil{
			return newEmpty[billy.File](), err
		}else{	
			COW=true
			COWPath=create_path
		}
		
		fileStat,_:=os.Stat(filename)
		fileMode:=fileStat.Mode()
		isRegular := fileMode.IsRegular() 
		isSymlink := (fileMode & fs1.ModeSymlink != 0)
		
		COW=COW && (isRegular || isSymlink) //Check that file is regular or is a symlink
		
		if COW {
			num_parent_dirs:=len(parentDirs(original_filename))
			originalParentDirs:=parentDirs(filename)
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
			
			src,_:=os.Open(filename)
			
			if isRegular{
				dest,_:=os.Create(COWPath)
			
				io.Copy(dest,src)
			}else if isSymlink{
				target,_ :=os.Readlink(filename)
				os.Symlink(target,COWPath)
			}
			
			setPermissions(filename, COWPath) //Make file with the same permissions as before
			
			filename=COWPath
		}else{
			flag=flag & ^(os.O_RDONLY | os.O_RDWR | os.O_WRONLY)
			flag |= os.O_RDONLY
		}
	}
	}	
	}
	open,err:=os.OpenFile(filename,flag,perm)
	
	if (flag & os.O_CREATE != 0){
		_,create_err:=os.Lstat(filename)
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

//The "Create" functions

func (fs OverlayFS) createErrorCheck(path string) {
	_,err:=os.Lstat(fs.findFirstExisting(path))
	if err==nil{
		fs.removefromDeleted(path)
	}
}
func (fs OverlayFS) Mknod(path string, mode uint32, major uint32, minor uint32) error {
	defer fs.createErrorCheck(path)
	
	filename,err:=fs.createPath(path)
	if err!=nil{
		return err
	}
	
	dev := unix.Mkdev(major, minor)
	return unix.Mknod(filename, mode, int(dev))
		
}

func (fs OverlayFS) Mkfifo(path string, mode uint32) error {
	defer fs.createErrorCheck(path)
	
	filename,err:=fs.createPath(path)
	if err!=nil{
		return err
	}
	
	return unix.Mkfifo(filename, mode)
}

func (fs OverlayFS) Link(link string, path string) error {
	defer fs.createErrorCheck(path)
	
	filename,err:=fs.createPath(path)
	if err!=nil{
		return err
	}
	
	return unix.Link(link, filename)
}

func (fs OverlayFS) Socket(path string) error {
	defer fs.createErrorCheck(path)
	
	filename,err:=fs.createPath(path)
	if err!=nil{
		return err
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
