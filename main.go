package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/golang/glog"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/extensions/volumeactions"
	cinder "github.com/gophercloud/gophercloud/openstack/blockstorage/v2/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/volumeattach"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/mitchellh/mapstructure"
	"github.com/spf13/cobra"
)

const (
	floating = "floating"
)

func init() {
	flag.Set("logtostderr", "true")
	// hack to make flag.Parsed return true such that glog is happy
	// about the flags having been parsed
	flag.CommandLine.Parse([]string{})
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "attacher",
		Short: "attacher",
		Long:  "attacher",
		Run: func(cmd *cobra.Command, args []string) {
			flag.Set("v", "2")
			glog.V(2).Infof("Starting application...\n")
			glog.Flush()
			Run()
		},
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func Run() {
	clients, err := GetOSClients()
	if err != nil {
		glog.Fatalf("%+v", err)
	}

	volumes, err := ListVolumes(clients.Volume)
	if err != nil {
		glog.Fatalf("%+v", err)
	}

	detach := false
	for _, volume := range volumes {
		if len(volume.Attachments) > 0 {
			detachOpts := volumeactions.DetachOpts{
				AttachmentID: volume.Attachments[0].AttachmentID,
			}
			detach = true
			glog.Infof("Detaching %s", volume.ID)
			err = volumeactions.Detach(clients.Volume, volume.ID, detachOpts).ExtractErr()
			if err != nil {
				glog.Fatalf("%+v", err)
			}
		}
	}

	if detach {
		glog.Infof("Waiting 60 secs for detaching the volumes")
		time.Sleep(60 * time.Second)
	}

	instances, err := ListInstances(clients.Compute)
	if err != nil {
		glog.Fatalf("%+v", err)
	}

	for i, instance := range instances {
		volume := volumes[i]
		glog.Infof("Attach %s to %s", volume.ID, instance.ID)
		go attach(clients.Compute, instance.ID, volumeattach.CreateOpts{
			VolumeID: volume.ID,
		})

		volume = volumes[i+3]
		glog.Infof("Attach %s to %s", volume.ID, instance.ID)
		go attach(clients.Compute, instance.ID, volumeattach.CreateOpts{
			VolumeID: volume.ID,
		})

	}
	glog.Infof("Waiting 60 secs for attach volumes")
	time.Sleep(60 * time.Second)

	username := "centos"
	if os.Getenv("ATTACHER_USERNAME") != "" {
		username = os.Getenv("ATTACHER_USERNAME")
	}
	for _, instance := range instances {
		cmd := exec.Command("ssh", fmt.Sprintf("%s@%s", username, instance.Addr), "ls -l /dev/vd* && ls /dev/disk/by-id/*")
		var out bytes.Buffer
		cmd.Stdout = &out
		var serr bytes.Buffer
		cmd.Stderr = &serr
		cmd.Run()
		glog.Infof("Instance ID %s", instance.ID)
		glog.Infof("stdout: \n%s", out.String())
		glog.Infof("stderr: \n%s\n", serr.String())
	}
}

func attach(client *gophercloud.ServiceClient, serverID string, opts volumeattach.CreateOpts) {
	_, err := volumeattach.Create(client, serverID, opts).Extract()
	if err != nil {
		glog.Errorf("Got error while attaching: %v", err)
	}
}

type Osutils struct {
	Compute *gophercloud.ServiceClient
	Volume  *gophercloud.ServiceClient
}

type instance struct {
	ID   string
	Addr string
}

// Address is struct for openstack instance interface addresses
type Address struct {
	IPType string `mapstructure:"OS-EXT-IPS:type"`
	Addr   string
}

func ListInstances(client *gophercloud.ServiceClient) ([]instance, error) {
	opt := servers.ListOpts{}
	allPages, err := servers.List(client, opt).AllPages()
	if err != nil {
		return nil, fmt.Errorf("error listing servers %v: %v", opt, err)
	}

	ss, err := servers.ExtractServers(allPages)
	if err != nil {
		return nil, fmt.Errorf("error extracting servers from pages: %v", err)
	}

	result := []instance{}
	for _, osInstance := range ss {
		address := ""
		for _, val := range osInstance.Addresses {
			var addresses []Address
			err := mapstructure.Decode(val, &addresses)
			if err != nil {
				return result, err
			}
			for _, addr := range addresses {
				if addr.IPType == floating {
					address = addr.Addr
					break
				}
			}
		}
		result = append(result, instance{
			ID:   osInstance.ID,
			Addr: address,
		})
	}
	return result, nil
}

func ListVolumes(client *gophercloud.ServiceClient) ([]cinder.Volume, error) {
	opt := cinder.ListOpts{}
	allPages, err := cinder.List(client, opt).AllPages()
	if err != nil {
		return nil, fmt.Errorf("error listing volumes %v: %v", opt, err)
	}

	vs, err := cinder.ExtractVolumes(allPages)
	if err != nil {
		return nil, fmt.Errorf("error extracting volumes from pages: %v", err)
	}

	return vs, nil
}

func GetOSClients() (*Osutils, error) {
	openstackUtils := &Osutils{}
	authOption := gophercloud.AuthOptions{
		IdentityEndpoint: os.Getenv("OS_AUTH_URL"),
		DomainName:       os.Getenv("OS_USER_DOMAIN_NAME"),
		Username:         os.Getenv("OS_USERNAME"),
		Password:         os.Getenv("OS_PASSWORD"),
		TenantID:         os.Getenv("OS_PROJECT_ID"),
		TenantName:       os.Getenv("OS_PROJECT_NAME"),
	}

	provider, err := openstack.NewClient(os.Getenv("OS_AUTH_URL"))
	if err != nil {
		return nil, fmt.Errorf("error building openstack provider client: %v", err)
	}

	err = openstack.Authenticate(provider, authOption)
	if err != nil {
		return nil, fmt.Errorf("error building openstack authenticated client: %v", err)
	}

	openstackUtils.Compute, err = openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Type: "compute",
	})
	if err != nil {
		return nil, fmt.Errorf("error building openstack compute client: %v", err)
	}

	openstackUtils.Volume, err = openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Type: "volumev3",
	})
	if err != nil {
		return nil, fmt.Errorf("error building openstack volume client: %v", err)
	}

	return openstackUtils, nil
}
