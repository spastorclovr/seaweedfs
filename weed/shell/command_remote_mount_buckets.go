package shell

import (
	"flag"
	"fmt"
	"github.com/chrislusf/seaweedfs/weed/filer"
	"github.com/chrislusf/seaweedfs/weed/pb/remote_pb"
	"github.com/chrislusf/seaweedfs/weed/remote_storage"
	"github.com/chrislusf/seaweedfs/weed/util"
	"io"
	"path/filepath"
)

func init() {
	Commands = append(Commands, &commandRemoteMountBuckets{})
}

type commandRemoteMountBuckets struct {
}

func (c *commandRemoteMountBuckets) Name() string {
	return "remote.mount.buckets"
}

func (c *commandRemoteMountBuckets) Help() string {
	return `mount all buckets in remote storage and pull its metadata

	# assume a remote storage is configured to name "cloud1"
	remote.configure -name=cloud1 -type=s3 -access_key=xxx -secret_key=yyy

	# mount all buckets
	remote.mount.buckets -remote=cloud1

	# after mount, start a separate process to write updates to remote storage
	weed filer.remote.sync -filer=<filerHost>:<filerPort> -createBucketAt=cloud1

`
}

func (c *commandRemoteMountBuckets) Do(args []string, commandEnv *CommandEnv, writer io.Writer) (err error) {

	remoteMountBucketsCommand := flag.NewFlagSet(c.Name(), flag.ContinueOnError)

	remote := remoteMountBucketsCommand.String("remote", "", "a already configured storage name")
	bucketPattern := remoteMountBucketsCommand.String("bucketPattern", "", "match existing bucket name with wildcard characters '*' and '?'")
	apply := remoteMountBucketsCommand.Bool("apply", false, "apply the mount for listed buckets")

	if err = remoteMountBucketsCommand.Parse(args); err != nil {
		return nil
	}

	if *remote == "" {
		_, err = listExistingRemoteStorageMounts(commandEnv, writer)
		return err
	}

	// find configuration for remote storage
	remoteConf, err := filer.ReadRemoteStorageConf(commandEnv.option.GrpcDialOption, commandEnv.option.FilerAddress, *remote)
	if err != nil {
		return fmt.Errorf("find configuration for %s: %v", *remote, err)
	}

	// get storage client
	remoteStorageClient, err := remote_storage.GetRemoteStorage(remoteConf)
	if err != nil {
		return fmt.Errorf("get storage client for %s: %v", *remote, err)
	}

	buckets, err := remoteStorageClient.ListBuckets()
	if err != nil {
		return fmt.Errorf("list buckets on %s: %v", *remote, err)
	}

	fillerBucketsPath, err := readFilerBucketsPath(commandEnv)
	if err != nil {
		return fmt.Errorf("read filer buckets path: %v", err)
	}

	for _, bucket := range buckets {
		if *bucketPattern != "" {
			if matched, _ := filepath.Match(*bucketPattern, bucket.Name); !matched {
				continue
			}
		}

		fmt.Fprintf(writer, "bucket %s\n", bucket.Name)
		if *apply {

			dir := util.FullPath(fillerBucketsPath).Child(bucket.Name)
			remoteStorageLocation := &remote_pb.RemoteStorageLocation{
				Name:   *remote,
				Bucket: bucket.Name,
				Path:   "/",
			}

			// sync metadata from remote
			if err = syncMetadata(commandEnv, writer, string(dir), true, remoteConf, remoteStorageLocation); err != nil {
				return fmt.Errorf("pull metadata on %+v: %v", remoteStorageLocation, err)
			}

			// store a mount configuration in filer
			if err = filer.InsertMountMapping(commandEnv, string(dir), remoteStorageLocation); err != nil {
				return fmt.Errorf("save mount mapping %s to %+v: %v", dir, remoteStorageLocation, err)
			}

		}
	}

	return nil
}