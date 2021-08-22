package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	config "github.com/ipfs/go-ipfs-config"
	files "github.com/ipfs/go-ipfs-files"
	"github.com/ipfs/go-ipfs/core"
	"github.com/ipfs/go-ipfs/core/coreapi"
	libp2p "github.com/ipfs/go-ipfs/core/node/libp2p"
	"github.com/ipfs/go-ipfs/plugin/loader"
	"github.com/ipfs/go-ipfs/repo/fsrepo"
	migrate "github.com/ipfs/go-ipfs/repo/fsrepo/migrations"
	icore "github.com/ipfs/interface-go-ipfs-core"
	"github.com/libp2p/go-libp2p-core/metrics"
	"github.com/libp2p/go-libp2p-core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/spf13/viper"
)

// Reporter of traffic data for the running IPFS node
var Reporter *metrics.BandwidthCounter

// IPFS node object
var ipfs icore.CoreAPI

// Whether or not an existing IPFS node should be used for pinning
var useExistingIPFSNode bool

// The path to an existing IPFS
var ipfsURI string

type ipfsAddResponse struct {
	Name string `json:"Name"`
	Hash string `json:"Hash"`
	Size string `json:"Size"`
}

// *** Functions from go-ipfs/docs/examples/go-ipfs-as-a-library ***

func setupPlugins(externalPluginsPath string) error {
	// Load any external plugins if available on externalPluginsPath
	plugins, err := loader.NewPluginLoader(filepath.Join(externalPluginsPath, "plugins"))
	if err != nil {
		return fmt.Errorf("error loading plugins: %s", err)
	}

	// Load preloaded and external plugins
	if err := plugins.Initialize(); err != nil {
		return fmt.Errorf("error initializing plugins: %s", err)
	}

	if err := plugins.Inject(); err != nil {
		return fmt.Errorf("error initializing plugins: %s", err)
	}

	return nil
}

/// ------ Spawning the node

// Creates an IPFS node and returns its coreAPI
func createNode(ctx context.Context, repoPath string) (icore.CoreAPI, error) {
	// Open the repo
	repo, err := fsrepo.Open(repoPath)
	missingRepoError := fsrepo.NoRepoError{
		Path: repoPath,
	}
	if err == fsrepo.ErrNeedMigration {
		err = migrate.RunMigration(fsrepo.RepoVersion)
		if err != nil {
			return nil, err
		}

		repo, err = fsrepo.Open(repoPath)
		if err != nil {
			return nil, err
		}
	} else if err == missingRepoError {
		// Create a config with default options and a 4096 bit key
		cfg, err := config.Init(ioutil.Discard, 4096)
		if err != nil {
			return nil, err
		}
		err = fsrepo.Init(repoPath, cfg)
		if err != nil {
			return nil, err
		}
		repo, err = fsrepo.Open(repoPath)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	// Add web socket listener
	config, err := repo.Config()
	if err != nil {
		return nil, err
	}
	config.Addresses.Swarm = []string{"/ip4/0.0.0.0/tcp/4001", "/ip6/::/tcp/4001", "/ip4/0.0.0.0/udp/4001/quic", "/ip6/::/udp/4001/quic", "/ip4/0.0.0.0/tcp/8081/ws", "/ip6/::/tcp/8081/ws"}
	repo.SetConfig(config)

	// Construct the node

	nodeOptions := &core.BuildCfg{
		Online:  true,
		Routing: libp2p.DHTOption, // This option sets the node to be a full DHT node (both fetching and storing DHT Records)
		// Routing: libp2p.DHTClientOption, // This option sets the node to be a client DHT node (only fetching records)
		Repo: repo,
	}

	node, err := core.NewNode(ctx, nodeOptions)
	if err != nil {
		return nil, err
	}

	Reporter = node.Reporter

	return coreapi.NewCoreAPI(node)
}

func spawnNode(ctx context.Context, ipfsRepoPath string) (icore.CoreAPI, error) {
	if err := setupPlugins(ipfsRepoPath); err != nil {
		return nil, err
	}

	return createNode(ctx, ipfsRepoPath)
}

func connectToPeers(ctx context.Context, ipfs icore.CoreAPI, peers []string) error {
	var wg sync.WaitGroup
	peerInfos := make(map[peer.ID]*peer.AddrInfo, len(peers))
	for _, addrStr := range peers {
		addr, err := ma.NewMultiaddr(addrStr)
		if err != nil {
			return err
		}
		pii, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			return err
		}
		pi, ok := peerInfos[pii.ID]
		if !ok {
			pi = &peer.AddrInfo{ID: pii.ID}
			peerInfos[pi.ID] = pi
		}
		pi.Addrs = append(pi.Addrs, pii.Addrs...)
	}

	wg.Add(len(peerInfos))
	for _, peerInfo := range peerInfos {
		go func(peerInfo *peer.AddrInfo) {
			defer wg.Done()
			err := ipfs.Swarm().Connect(ctx, *peerInfo)
			if err != nil {
				log.Printf("failed to connect to %s: %s", peerInfo.ID, err)
			}
		}(peerInfo)
	}

	wg.Wait()
	return nil
}

func getUnixfsNode(path string) (files.Node, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	f, err := files.NewSerialFile(path, false, st)
	if err != nil {
		return nil, err
	}

	return f, nil
}

/// ------
// *** End of functions from go-ipfs/docs/examples/go-ipfs-as-a-library ***

func addFolderToIPFS(ctx context.Context, path string) (string, error) {
	if useExistingIPFSNode {
		return addFolderToRemoteIPFS(path)
	} else {
		return addFolderToDapperIPFS(ctx, path)
	}
}

func addFolderToDapperIPFS(ctx context.Context, path string) (string, error) {
	someDirectory, err := getUnixfsNode(path)
	if err != nil {
		return "", err
	}

	cidDirectory, err := ipfs.Unixfs().Add(ctx, someDirectory)
	if err != nil {
		return "", err
	}

	err = ipfs.Pin().Add(ctx, cidDirectory)
	if err != nil {
		return "", err
	}

	return strings.Split(cidDirectory.String(), "/")[2], nil
}

func addFolderToRemoteIPFS(videoFolder string) (string, error) {
	client := http.Client{}
	// Prepare a form that you will submit to that URL.
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	values, err := openFilesInDir(videoFolder)
	if err != nil {
		return "", err
	}
	for i, r := range values {
		var fw io.Writer
		if x, ok := r.(io.Closer); ok {
			defer x.Close()
		}
		// Add a file
		if x, ok := r.(*os.File); ok {
			// Get the last 2 parts of the file name
			// This will result in the folder the file is stored in and the file itself
			fileParts := strings.Split(x.Name(), "/")
			if fw, err = w.CreateFormFile("video"+fmt.Sprintf("%d", i), path.Join(fileParts[len(fileParts)-2], fileParts[len(fileParts)-1])); err != nil {
				return "", err
			}
		} else {
			// Ignored for now
			// Add non-file fields
			if fw, err = w.CreateFormField("video" + fmt.Sprintf("%d", i)); err != nil {
				return "", err
			}
		}
		if _, err = io.Copy(fw, r); err != nil {
			return "", err
		}

	}
	// Don't forget to close the multipart writer.
	// If you don't close it, your request will be missing the terminating boundary.
	w.Close()

	// Now that you have a form, you can submit it to your handler.
	req, err := http.NewRequest("POST", ipfsURI+"/api/v0/add", &b)
	if err != nil {
		return "", err
	}
	// Don't forget to set the content type, this will contain the boundary.
	req.Header.Set("Content-Type", w.FormDataContentType())

	// Submit the request
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}

	// Check the response
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("Failed reading body of ipfs response: %s\n", err)
		return "", err
	}

	if res.StatusCode >= 400 {
		fmt.Printf("Error from ipfs: %s\n", string(body))
		return "", err
	}

	// Body must be split into individual json objects since what is returned now is not a valid json object
	bodyParts := strings.Split(string(body), "\n")

	// The second to last object in this list is the pinned folder
	var folderResponse ipfsAddResponse
	err = json.Unmarshal([]byte(bodyParts[len(bodyParts)-2]), &folderResponse)
	if err != nil {
		return "", err
	}
	return folderResponse.Hash, nil
}

func openFilesInDir(path string) ([]io.Reader, error) {
	var files []string
	var fileReaders []io.Reader

	root := path
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		fileReader, err := mustOpen(file)
		if err != nil {
			return nil, err
		}
		fileReaders = append(fileReaders, fileReader)
	}

	return fileReaders, nil
}

func mustOpen(f string) (*os.File, error) {
	r, err := os.Open(f)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func addFileToDapperIPFS(ctx context.Context, path string) (string, error) {
	someFile, err := getUnixfsNode(path)
	if err != nil {
		return "", err
	}

	cidFile, err := ipfs.Unixfs().Add(ctx, someFile)
	if err != nil {
		return "", err
	}

	err = ipfs.Pin().Add(ctx, cidFile)
	if err != nil {
		return "", err
	}

	return strings.Split(cidFile.String(), "/")[2], nil
}

func startIPFS(ctx context.Context) error {
	// Read config data
	ipfsURI = viper.GetString("IPFS.ipfsURI")
	ipfsRepoPath := viper.GetString("IPFS.ipfsRepoDir")
	if ipfsRepoPath == "" {
		ipfsRepoPath, _ = config.PathRoot()
	}

	// If a IPFS URI has not been defined,
	// check if a node is running on the system.
	if ipfsURI == "" {
		defaultIPFSRoot, err := config.PathRoot()
		if err != nil {
			return err
		}
		ipfsRunning, err := checkIPFSDirLocked(defaultIPFSRoot)
		if err != nil {
			return err
		}
		if ipfsRunning {
			useExistingIPFSNode = true
			ipfsURI = "http://localhost:5001"
		}

		ipfsRunning, err = checkIPFSDirLocked(ipfsRepoPath)
		if err != nil {
			return err
		}
		if ipfsRunning {
			useExistingIPFSNode = true
			ipfsURI = "http://localhost:5001"
		}

		ipfsRunning, err = checkIPFSListeningLocalhost()
		if err != nil {
			return err
		}
		if ipfsRunning {
			useExistingIPFSNode = true
			ipfsURI = "http://localhost:5001"
		}

		// If not using an existing IPFS Node, we need to start one
		if !useExistingIPFSNode {
			// Spawn a node using the path specified in the config.
			// If no path was specified in the config, the default path for IPFS is used ($HOME/.ipfs)
			// If the repo at the path does not exists, it is initialized
			ipfsTmp, err := spawnNode(ctx, ipfsRepoPath)
			if err != nil {
				return err
			}

			fmt.Println("Internal IPFS node is running")

			bootstrapNodes := []string{
				// IPFS Bootstrapper nodes.
				"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
				"/dnsaddr/bootstrap.libp2p.io/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa",
				"/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb",
				"/dnsaddr/bootstrap.libp2p.io/p2p/QmcZf59bWwK5XFi76CZX8cbJ4BhTzzA3gU1ZjYZcYW3dwt",

				// IPFS Cluster Pinning nodes
				"/ip4/138.201.67.219/tcp/4001/p2p/QmUd6zHcbkbcs7SMxwLs48qZVX3vpcM8errYS7xEczwRMA",
				"/ip4/138.201.67.219/udp/4001/quic/p2p/QmUd6zHcbkbcs7SMxwLs48qZVX3vpcM8errYS7xEczwRMA",
				"/ip4/138.201.67.220/tcp/4001/p2p/QmNSYxZAiJHeLdkBg38roksAR9So7Y5eojks1yjEcUtZ7i",
				"/ip4/138.201.67.220/udp/4001/quic/p2p/QmNSYxZAiJHeLdkBg38roksAR9So7Y5eojks1yjEcUtZ7i",
				"/ip4/138.201.68.74/tcp/4001/p2p/QmdnXwLrC8p1ueiq2Qya8joNvk3TVVDAut7PrikmZwubtR",
				"/ip4/138.201.68.74/udp/4001/quic/p2p/QmdnXwLrC8p1ueiq2Qya8joNvk3TVVDAut7PrikmZwubtR",
				"/ip4/94.130.135.167/tcp/4001/p2p/QmUEMvxS2e7iDrereVYc5SWPauXPyNwxcy9BXZrC1QTcHE",
				"/ip4/94.130.135.167/udp/4001/quic/p2p/QmUEMvxS2e7iDrereVYc5SWPauXPyNwxcy9BXZrC1QTcHE",
			}

			go connectToPeers(ctx, ipfsTmp, bootstrapNodes)

			ipfs = ipfsTmp
		} else {
			fmt.Println("Using existing IPFS node on localhost")
		}
	} else {
		useExistingIPFSNode = true
	}

	fmt.Println("IPFS Ready!")

	return nil
}

func checkIPFSDirLocked(ipfsRepo string) (bool, error) {
	locked, err := fsrepo.LockedByOtherProcess(ipfsRepo)
	if err != nil {
		return false, err
	}
	if locked {
		return true, nil
	}

	return false, nil
}

func checkIPFSListeningLocalhost() (bool, error) {
	client := http.Client{}
	req, err := http.NewRequest(http.MethodPost, "http://localhost:5001/api/v0/id", nil)

	if err != nil {
		return false, err
	}

	resp, err := client.Do(req)

	// Assume any error in performing the request means IPFS is not running
	if err != nil {
		return false, nil
	}

	defer resp.Body.Close()

	// Assume any response error codes means IPFS is not running
	if resp.StatusCode >= 400 {
		return false, nil
	}

	return true, nil
}
