package main

import(
	"os"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
)

//The "Create" functions

func (fs OverlayFS) createErrorCheck(path string) {
	_,err:=os.Lstat(fs.findFirstExisting(path))
	if err==nil{
		fs.removefromDeleted(path)
	}else{
		fmt.Println("createErrorCheck: ",path)
		fmt.Println("createErrorCheck Error: ",err)
	}
	fmt.Println("Error check completed!")
}

func (fs OverlayFS) Rename(oldpath, newpath string) error{

	old,err:=fs.Open(oldpath)
	if err!=nil{
		return err
	}
	
	new,err:=fs.OpenFile(newpath,os.O_CREATE|os.O_RDWR,0700) //Activate COW
	if err!=nil{
		return err
	}
	
	if fs.getModeofFirstExisting(oldpath)=="RO"{
		_,err:=io.Copy(new,old)
		fs.Remove(oldpath)
		return err
	}
	
	return os.Rename(fs.findFirstExisting(oldpath),fs.findFirstExisting(newpath))
		
}
func (fs OverlayFS) Mkdir(path string, perm os.FileMode) error {
	defer fs.createErrorCheck(path)
	
	fmt.Println("Mkdir:",path)
	
	filename,err:=fs.createPath(path)
	if err!=nil{
		return err
	}
	
	if _, err := os.Stat(filename); !os.IsNotExist(err) { //Don't create if it already exists
		return nil
	}
	
	return os.Mkdir(filename, perm)
		
}

func (fs OverlayFS) MkdirAll(path string, perm os.FileMode) error {
	fmt.Println("MkdirAll:",path)
	
	dirs:=parentDirs(path)
	
	dirs=append(dirs,path)

	for _, dir := range dirs{
		err:=fs.Mkdir(dir,perm)
		if err!=nil{
			return err
		}
	}
	
	return nil
		
}

func (fs OverlayFS) Mknod(path string, mode uint32, major uint32, minor uint32) error {
	defer fs.createErrorCheck(path)
	
	fmt.Println("Mknod:",path)
	
	filename,err:=fs.createPath(path)
	if err!=nil{
		return err
	}
	
	dev := unix.Mkdev(major, minor)
	return unix.Mknod(filename, mode, int(dev))
		
}

func (fs OverlayFS) Mkfifo(path string, mode uint32) error {
	defer fs.createErrorCheck(path)
	
	fmt.Println("Mkfifo:",path)
	
	filename,err:=fs.createPath(path)
	if err!=nil{
		return err
	}
	
	return unix.Mkfifo(filename, mode)
}

func (fs OverlayFS) Link(link string, path string) error {
	defer fs.createErrorCheck(path)
	
	fmt.Println("Link:",path)
	
	filename,err:=fs.createPath(path)
	if err!=nil{
		return err
	}
	
	return unix.Link(fs.findFirstExisting(link), filename)
}

func (fs OverlayFS) Symlink(link string, path string) error {
	defer fs.createErrorCheck(path)
	
	fmt.Println("Symlink:",path)
	
	filename,err:=fs.createPath(path)
	if err!=nil{
		fmt.Println("Symlink error: ",err)
		return err
	}
	
	return unix.Symlink(link, filename)
}

func (fs OverlayFS) Socket(path string) error {
	defer fs.createErrorCheck(path)
	
	fmt.Println("Socket:",path)
	
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