package main
import (
	"os"
	"fmt"
	"path/filepath"
	"path"
)

func (fs OverlayFS) getTarget(filename string) string{
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
	
	return filename
}

func (fs OverlayFS) Stat(filename string) (os.FileInfo, error){
	if fs.checkIfDeleted(filename){
		return newEmpty[os.FileInfo](), os.ErrNotExist
	}
	
	fmt.Println("Stat:",filename)
	
	//NFS makes no distinction between Lstat and Stat so the following code had to be commented out.
	//filename=fs.getTarget(filename)
	return fs.Lstat(filename)
}

func (fs OverlayFS) Lstat(filename string) (os.FileInfo, error){
	if fs.checkIfDeleted(filename){
		return newEmpty[os.FileInfo](), os.ErrNotExist
	}
	
	fmt.Println("Lstat:",filename)

	result, err:=os.Lstat(fs.findFirstExisting(filename))
	fmt.Println("Lstat finished!")
	return OverlayStat{result,result,filename,fs}, err
}

func (fs OverlayFS) Readlink(link string) (string, error){
	 if fs.checkIfDeleted(link){
		 return "",os.ErrNotExist
	 }
	 
	 fmt.Println("Readlink:",link)
	 
	 return os.Readlink(fs.findFirstExisting(link))
}
	 
func (fs OverlayFS) Chown(name string, uid, gid int) error{
	if fs.checkIfDeleted(name){
		return os.ErrNotExist
	}
	
	fmt.Println("Chown:",name)
	return fs.Lchown(fs.getTarget(name), uid, gid)
}

func (fs OverlayFS) Lchown(name string, uid, gid int) error{
	if fs.checkIfDeleted(name){
		return os.ErrNotExist
	}
	
	fmt.Println("Lchown:",name)
	return os.Lchown(fs.findFirstExisting(name),uid,gid)
}