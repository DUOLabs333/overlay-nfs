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
	}
	fmt.Println("Error check completed!")
}

func (fs OverlayFS) Rename(oldpath, newpath string) error{

	old,err:=fs.Open(oldpath)
	if err!=nil{
		return err
	}
	
	_,err=fs.Stat(newpath)
	
	if err!=nil{
		return err
	}
	
	if err==nil && fs.getModeofFirstExisting(newpath)=="RO"{
		new,_:=fs.OpenFile(newpath,os.O_WRONLY,0700) //Activate COW
		_,err:=io.Copy(new,old)
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
	return os.Mkdir(filename, perm)
		
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
	
	return unix.Link(link, filename)
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