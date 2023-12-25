package main

import(
	"os"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
)

//The "Create" functions

func (fs OverlayFS) createErrorCheck(create_path, path string) {
	_,err:=os.Lstat(create_path)
	if err==nil{
		fs.removefromDeleted(path)
	}else{
		fmt.Println("createErrorCheck: ",path)
		fmt.Println("createErrorCheck Error: ",err)
	}
	fmt.Println("Error check completed!")
}

func (fs OverlayFS) Rename(oldpath, newpath string) error{
	if !fs.checkIfExists(oldpath){
		return os.ErrNotExist
	}
	fmt.Println("Rename: ",newpath)
	
	newPath,err:=fs.createPath(newpath)
	if err!=nil{
		return err
	}
	
	oldStat,_:=fs.Lstat(oldpath)
	oldMode:=fs.getModeofFirstExisting(oldpath)
	oldPath:=fs.findFirstExisting(oldpath)
	
	if !oldStat.IsDir(){
		if oldMode=="RW"{ //Can remove from oldpath
			os.Rename(oldPath,newPath)
		}else{
			err:=fs.Remove(newpath) //This matters when dealing with non-(regular files), eg. directories.
			if err!=nil && !os.IsNotExist(err){
				return err
			}
			
			if oldStat.Mode().IsRegular(){
				old,err:=fs.Open(oldpath)
				if err!=nil{
					return err
				}
				
				new,err:=fs.OpenFile(newpath,os.O_CREATE|os.O_TRUNC|os.O_WRONLY,0666)
				if err!=nil{
					return err
				}
				
				_,err=io.Copy(new, old)
				new.Close()
				if err!=nil{
					return err
				}
			} else if oldStat.Mode() & os.ModeSymlink != 0 {
				
				oldTarget, err:=fs.Readlink(oldpath)
				if err!=nil{
					return err
				}
				
				err=fs.Symlink(oldTarget, newpath)
				if err != nil{
					return err
				}
			}else{ //If it's something like a device, I just don't want to deal with all the different options. I may deal with them later if it becomes an issue
				return unspecifiedError
			}
		}
	}else{
		err=fs.Remove(newpath)
		if err!=nil && !os.IsNotExist(err){
			return err
		}
		
		err=fs.Mkdir(newpath,0666)
		if err!=nil && !os.IsExist(err){
			return err
		}
		
		items, err:=fs.ReadDir(oldpath)
		if err!=nil{
			return err
		}
		for _, item:= range items{
			name:=item.Name()
			err=fs.Rename(fs.Join(oldpath,name),fs.Join(newpath,name))
			if err!=nil{
				return err
			}
		}
	}
	
	if !(oldMode=="RW" && !oldStat.IsDir()){ //So there was no os.Rename
		setPermissions(oldPath,newPath)
		err:=fs.Remove(oldpath)
		if err!=nil{
			return err
		}
	}
	
	return nil
		
}
func (fs OverlayFS) Mkdir(path string, perm os.FileMode) error {
	fmt.Println("Mkdir:",path)
	
	filename,err:=fs.createPath(path)
	if err!=nil{
		return err
	}
	
	if _, err := os.Lstat(filename); !os.IsNotExist(err) { //Don't create if it already exists
		return nil
	}
	
	defer fs.createErrorCheck(filename,path)
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
	
	fmt.Println("Mknod:",path)
	
	filename,err:=fs.createPath(path)
	if err!=nil{
		return err
	}
	
	dev := unix.Mkdev(major, minor)
	
	defer fs.createErrorCheck(filename,path)
	return unix.Mknod(filename, mode, int(dev))
		
}

func (fs OverlayFS) Mkfifo(path string, mode uint32) error {
	
	fmt.Println("Mkfifo:",path)
	
	filename,err:=fs.createPath(path)
	if err!=nil{
		return err
	}
	
	defer fs.createErrorCheck(filename,path)
	return unix.Mkfifo(filename, mode)
}

func (fs OverlayFS) Link(link string, path string) error {
	if !fs.checkIfExists(link){
		return nil
	}
	
	fmt.Println("Link:",path)
	
	filename,err:=fs.createPath(path)
	if err!=nil{
		return err
	}
	
	defer fs.createErrorCheck(filename,path)
	return unix.Link(fs.findFirstExisting(link), filename)
}

func (fs OverlayFS) Symlink(link string, path string) error {
	fmt.Println("Symlink:",path)
	
	filename,err:=fs.createPath(path)
	if err!=nil{
		fmt.Println("Symlink error: ",err)
		return err
	}
	
	defer fs.createErrorCheck(filename,path)
	return unix.Symlink(link, filename)
}

func (fs OverlayFS) Socket(path string) error {
	
	fmt.Println("Socket:",path)
	
	filename,err:=fs.createPath(path)
	if err!=nil{
		return err
	}
	
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return err
	}
	
	defer fs.createErrorCheck(filename,path)
	return unix.Bind(fd, &unix.SockaddrUnix{Name: filename})
}