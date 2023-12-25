package main

import(

	billy "github.com/go-git/go-billy/v5"
	
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"syscall"
	"strconv"
	"encoding/json"
	"strings"
	"sort"
	"math/rand"
	"hash/fnv"
	"errors"
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
	dirInoMap map[string]uint64
	
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

var unspecifiedError error = errors.New("H") //Just an arbitary error

type OverlayStat struct{
	 os.FileInfo
	 file os.FileInfo
	 path string
	 fs OverlayFS
}


func hashString(s string) uint64 {
		h := fnv.New64a()
		h.Write([]byte(s))
		return h.Sum64()
}

func (stat OverlayStat) Sys() any{
	original:=stat.file.Sys()
	info, ok:=original.(*syscall.Stat_t)
	if !ok{
		return original
	}
	
	if !stat.IsDir(){
		return original
	}
	
	ino, exists := stat.fs.dirInoMap[stat.path]
	if !exists{//The inode of a directory can change udring the course of a mount due to COW. This keeps it static by just using the hash. It should be unique enough, and if needed, we can implement more thorough checks for things like symlinks.
		ino=hashString(stat.path)
		stat.fs.dirInoMap[stat.path]=ino
	}
	
	info.Ino=ino
	
	return info
	
} 

func NewFS(options []string, mountpoint string) (OverlayFS){
	result:=OverlayFS{}
	
	result.paths=make([]string,0)
	result.modes=make([]string,0)
	result.mountpoint=mountpoint
	result.deletedMap=make(map[string]bool)
	result.dirInoMap=make(map[string]uint64)
	
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

func (fs OverlayFS) IsEqual( a any) bool{
	b,_:=a.(OverlayFS)
	return fs.mountpoint==b.mountpoint
}

func setPermissions(src, dest string) {
	srcStat,err:=os.Lstat(src)
	if err!=nil{
		fmt.Println(err)
	}
	
	os.Chmod(dest,srcStat.Mode())
	
	srcUid:=-1
	srcGid:=-1
	
	if srcInfo, ok := srcStat.Sys().(*syscall.Stat_t); ok{
		srcUid=int(srcInfo.Uid)
		srcGid=int(srcInfo.Gid)
	}
	
	os.Lchown(dest,srcUid,srcGid)
}

func parentDirs(Path string) ([]string){
	parent_dirs:=make([]string,0)
	curr:=filepath.Dir(Path)
	
	for true{
		if len(parent_dirs)>0 && curr==parent_dirs[len(parent_dirs)-1]{ //Otherwise would lead to infinite loop
			break
		}
		
		parent_dirs=append(parent_dirs,curr)
		curr=filepath.Dir(curr)
	}
	
	parent_dirs=parent_dirs[0:len(parent_dirs)-1] //We remove the last element (typically '.' or '/') as we don't need it, and we never get to that point.
	slices.Reverse(parent_dirs)
	
	return parent_dirs
}

func (fs OverlayFS) createPath(filename string) (string, error){ //If file is deleted and findFirstExisting returns an existing file, then createPath can only return those above the existing file. Else, if it exists, just use that.

	//Else, if the parent dir does not exist (check with fs.Lstat), return an error (you can not create a file if the parent directory does not exist). Else, mkdirall on the the RW directory that has the most elements (if there's a tie, randomly pick one).
	
	//The returned path should be everything (including the file, not just the parent directory)
	
	fileExists:=fs.checkIfExists(filename)
	
	original_path:=fs.findFirstExisting(filename)
	_,err:=os.Lstat(original_path)
	original_path_exists:=(err==nil)

	if !fs.checkIfExists(filepath.Dir(filename)){ //You can not create a file if the parent directory does not exist
		return "", os.ErrNotExist
	}
	
	if fileExists && fs.getModeofFirstExisting(filename)=="RW"{ //No need to create another one
		return original_path, nil
	}
	
	pathMap:=make(map[int][]string)
	
	pathMap[-1]=make([]string,0)
	
	for i, dir := range fs.paths{
		if !fileExists && original_path_exists && (fs.Join(dir,filename)==original_path){ //If the file exists, then any possible replacement file has to be above to take precedence. If you get to this point, no such file has been found.
			break
		}
		
		if fs.modes[i]!="RW"{
			continue
		}
		
		parent_dirs:=parentDirs(filename)
		slices.Reverse(parent_dirs)
		
		idx:=-1
		
		for j, _ :=range parent_dirs{
			_, err:= os.Lstat(fs.Join(dir,parent_dirs[j]))
			if err==nil{
				idx=j
				break
			}
		}
		
		arr, exists := pathMap[idx]
		if !exists{
			arr=make([]string,0)
		}
		
		arr=append(arr,dir)
		pathMap[idx]=arr
	}
	
	keys := make([]int, 0)
	 
	for k := range pathMap{
	    keys = append(keys, k)
	}
	sort.Ints(keys)
	
	dirs:=pathMap[keys[0]]
	
	if len(keys)>1{ //Pick the directories with the most number of subdirectories already created
		dirs=pathMap[keys[1]]
	}
	
	possible_dir:=""
	
	if len(dirs)==0{ //No suitable directories
		return "", os.ErrNotExist
	}else{
		possible_dir=dirs[rand.Intn(len(dirs))] //Randomly pick a directory to balance out the load
	}
	
	err=os.MkdirAll(fs.Join(possible_dir,filepath.Dir(filename)),0700)
	
	return fs.Join(possible_dir,filename), err
}
func (fs OverlayFS) joinRelative(Path string) ([]string){
	result:=make([]string,0,len(fs.paths))
	for i, _ := range fs.paths{
		result=append(result,path.Join(fs.paths[i],Path))
	}
	return result
}

func (fs OverlayFS) indexOfFirstExisting(filename string) int{
	possibleFiles:=fs.joinRelative(filename)
	for i, file := range possibleFiles{
		_,err:=os.Lstat(file)
		if err==nil{
			return i
		}
	}
	return len(possibleFiles)-1
}

func (fs OverlayFS) findFirstExisting(filename string) string{
	possibleFiles:=fs.joinRelative(filename)
	return possibleFiles[fs.indexOfFirstExisting(filename)]
}


func (fs OverlayFS) getModeofFirstExisting(filename string) string{
	 return fs.modes[fs.indexOfFirstExisting(filename)]
}

func (fs OverlayFS) checkIfDeleted(filename string) bool{
	paths:=parentDirs(filename)
	
	paths=append(paths,filename)
	
	for i, _ := range paths{
		_, exists := fs.deletedMap[paths[i]]
		if exists{
			fmt.Println("Deleted:", paths[i])
			return true
		}
	}
	return false
		
}

func (fs OverlayFS) checkIfExists(filename string) bool{
	_,err:=fs.Lstat(filename)
	return (err==nil)
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
	_, exists:= fs.deletedMap[filename]
	
	if !exists{ //If not in map, no need to try to remove it 
		return
	}
		
	delete(fs.deletedMap,filename)
	fs.writeToDeletedMapFile()
}