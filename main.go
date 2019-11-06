package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/golang/glog"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/spf13/cobra"
	cinder "github.com/gophercloud/gophercloud/openstack/blockstorage/v2/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/volumeattach"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/extensions/volumeactions"
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

	for _, volume := range volumes {
		if len(volume.Attachments) > 0 {
			detachOpts := volumeactions.DetachOpts{
				AttachmentID: volume.Attachments[0].AttachmentID,
			}

			err = volumeactions.Detach(clients.Volume, volume.ID, detachOpts).ExtractErr()
			if err != nil {
				glog.Fatalf("%+v", err)
			}
		}
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
	time.Sleep(30 * time.Second)
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

func ListInstances(client *gophercloud.ServiceClient) ([]servers.Server, error)  {
	opt := servers.ListOpts{}
	allPages, err := servers.List(client, opt).AllPages()
	if err != nil {
		return nil, fmt.Errorf("error listing servers %v: %v", opt, err)
	}

	ss, err := servers.ExtractServers(allPages)
	if err != nil {
		return nil, fmt.Errorf("error extracting servers from pages: %v", err)
	}
	return ss, nil
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
