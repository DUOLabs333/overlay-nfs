package main
import (
	"os"
	"fmt"
	"path/filepath"
	"strings"
)

func (fs OverlayFS) getTarget(filename string) string{

	inputDirs:=strings.Split(filename,string(os.PathSeparator))
	outputDir:=""
	
	for i, _ := range inputDirs {
		dir:=fs.Join(outputDir,inputDirs[i])
		if !fs.checkIfExists(dir){
			tempSlice:=append([]string{dir},inputDirs[i+1:len(inputDirs)]...)
			outputDir=fs.Join(tempSlice...) //No point in continuing
			break
		}
		for true {
			target, err:=os.Readlink(fs.findFirstExisting(filename))
			if err!=nil{ //It's not a link
				outputDir=dir
				break
			}else{
				if !filepath.IsAbs(target){
					dir=fs.Join(filepath.Dir(dir),target)
				}else{
					dir=target
				}
			}
		}
	}
	return outputDir
}

func (fs OverlayFS) Stat(filename string) (os.FileInfo, error){
	fmt.Println("Stat:",filename)
	
	//NFS makes no distinction between Lstat and Stat so the following code had to be commented out.
	//filename=fs.getTarget(filename)
	return fs.Lstat(filename)
}

func (fs OverlayFS) Lstat(filename string) (os.FileInfo, error){
	if !fs.checkIfExists(filename){
		return nil, os.ErrNotExist
	}
	
	fmt.Println("Lstat:",filename)
	
	filename=fs.Join(fs.getTarget(filepath.Dir(filename)),filepath.Base(filename)) //Resolve everything except for the last part
	if !fs.checkIfExists(filename){
		return nil, os.ErrNotExist
	}
	
	result, err:=os.Lstat(fs.findFirstExisting(filename))
	fmt.Println("Lstat finished!")
	return OverlayStat{result,result,filename,fs}, err
}

func (fs OverlayFS) Readlink(filename string) (string, error){
	 if !fs.checkIfExists(filename){
		 return "",os.ErrNotExist
	 }
	 
	 fmt.Println("Readlink:",filename)
	 
	 filename=fs.Join(fs.getTarget(filepath.Dir(filename)),filepath.Base(filename)) //Resolve everything except for the last part
	 if !fs.checkIfExists(filename){
		 return "",os.ErrNotExist
	 }
	 
	 return os.Readlink(fs.findFirstExisting(filename))
}
	 
func (fs OverlayFS) Chown(name string, uid, gid int) error{
	if !fs.checkIfExists(name){
		return os.ErrNotExist
	}
	
	fmt.Println("Chown:",name)
	
	if !fs.checkIfExists(fs.getTarget(name)){
		return os.ErrNotExist
	}
	return fs.Lchown(fs.getTarget(name), uid, gid)
}

func (fs OverlayFS) Lchown(filename string, uid, gid int) error{
	if !fs.checkIfExists(filename){
		return os.ErrNotExist
	}
	fmt.Println("Lchown:",filename)
	
	filename=fs.Join(fs.getTarget(filepath.Dir(filename)),filepath.Base(filename)) //Resolve everything except for the last part
	if !fs.checkIfExists(filename){
		return os.ErrNotExist
	}
	
	fs.OpenFile(filename,os.O_RDWR,0700) //Activates COW if needed
	
	return os.Lchown(fs.findFirstExisting(filename),uid,gid)
}

func (fs OverlayFS) Chmod(filename string, mode os.FileMode) error{
	if !fs.checkIfExists(filename){
		return os.ErrNotExist
	}
	fmt.Println("Chmod:", filename)
	
	filename=fs.getTarget(filename) //Chmod acts on files, not symlinks
	if !fs.checkIfExists(filename){
		return os.ErrNotExist
	}
	
	fs.OpenFile(filename,os.O_RDWR,0700) //Activates COW if needed
	
	return os.Chmod(fs.findFirstExisting(filename),mode)
}