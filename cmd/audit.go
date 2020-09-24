package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/xanzy/go-gitlab"
	"log"
	"os"
	"sync"
	"time"
)

type genericMember struct {
	Type               string
	ContainerID        int
	Path               string
	Username           string
	UserId             int
	AccessLevel        gitlab.AccessLevelValue
	ExpiresAt          *gitlab.ISOTime
	MembersSettingsURL string
}

type memberFilter func(m *genericMember) bool

var (
	gitlabToken   string
	gitlabGroupId string
	excludedUsers []string
	sem           = make(chan bool, 5)
	wg            = &sync.WaitGroup{}

	git *gitlab.Client

	defaultListOptions = gitlab.ListOptions{
		Page:    1,
		PerPage: 10,
	}
)

var memberFilters = []memberFilter{
	// Add functions to this slice in order to apply additional filters to the output
	func(m *genericMember) bool {
		return true
	},
}

func NewAuditCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gitlabuseraudit",
		Short: "Generate report of users with non-inherited permissions",
		Long:  `Reports on any users that have their permissions set explicitly at a Group or Project level and not inherited from the parent Group`,
		Run:   gitlabUserAudit,
	}
	cmd.Flags().StringVarP(&gitlabToken, "gitlabToken", "t", "", "Gitlab API Access Token")
	cmd.MarkFlagRequired("gitlabToken")

	cmd.Flags().StringVar(&gitlabGroupId, "gid", "", "Gitlab GroupId")
	cmd.MarkFlagRequired("gitlabGroupID")

	cmd.Flags().StringSliceVar(&excludedUsers, "excludedUsers", []string{}, "Users to ignore from output")

	return cmd
}

func init() {

}

func gitlabUserAudit(cmd *cobra.Command, args []string) {
	start := time.Now()

	// For each users in the excluded list, add a filter to the memberFilters slice where that users has Owner access
	// this is to exclude known exceptions, like myself being an owner for a lot of repos.
	for _, username := range excludedUsers {
		memberFilters = append(memberFilters, func(m *genericMember) bool {
			return !(m.Username == username && m.AccessLevel == gitlab.OwnerPermission)
		})
	}

	git = gitlab.NewClient(nil, gitlabToken)

	grp, _, err := git.Groups.GetGroup(gitlabGroupId)
	if err != nil {
		log.Fatal(err)
	}

	if err = processGroup(grp); err != nil {
		log.Fatal(err)
	}

	wg.Wait()
	fmt.Println(time.Now().Sub(start))
}

// processGroup will loop through all the projects in this group, it will then loop through any subgroups
// calling processGroup recursively.
func processGroup(grp *gitlab.Group) error {

	sem <- true
	wg.Add(1)

	// Process all the Projects in a group
	listGroupProjectsOpts := &gitlab.ListGroupProjectsOptions{
		ListOptions: defaultListOptions,
	}

	for {
		grpProjects, res, err := git.Groups.ListGroupProjects(grp.ID, listGroupProjectsOpts)
		if err != nil {
			return err
		}

		for _, p := range grpProjects {
			if err := processProjectMembersPermissions(p); err != nil {
				return err
			}
		}

		if res.CurrentPage >= res.TotalPages {
			break
		}

		listGroupProjectsOpts.Page = res.NextPage
	}

	// Process and sub groups in the group
	listSubgroupsOptions := &gitlab.ListSubgroupsOptions{
		ListOptions: defaultListOptions,
	}

	for {
		grpSubGroups, res, err := git.Groups.ListSubgroups(grp.ID, listSubgroupsOptions)
		if err != nil {
			return err
		}

		for _, g := range grpSubGroups {
			if err := processGroupMembersPermissions(g); err != nil {
				return err
			}

			go func() {
				// recursive call "self" to process all the projects and subgroups that might exist in g
				if err := processGroup(g); err != nil {
					log.Fatal(err)
				}
			}()
		}

		if res.CurrentPage >= res.TotalPages {
			break
		}

		listSubgroupsOptions.Page = res.NextPage
	}

	<-sem
	wg.Done()
	return nil
}

func processProjectMembersPermissions(p *gitlab.Project) error {
	listProjectMembersOptions := &gitlab.ListProjectMembersOptions{
		ListOptions: defaultListOptions,
	}

	for {
		members, resp, err := git.ProjectMembers.ListProjectMembers(p.ID, listProjectMembersOptions)
		if err != nil {
			return err
		}

		for _, m := range members {
			handleMember(p, m)
		}

		if resp.CurrentPage >= resp.TotalPages {
			break
		}

		listProjectMembersOptions.Page = resp.NextPage
	}
	return nil
}

func processGroupMembersPermissions(g *gitlab.Group) error {

	listGroupMembersOptions := &gitlab.ListGroupMembersOptions{
		ListOptions: defaultListOptions,
	}

	for {
		members, resp, err := git.Groups.ListGroupMembers(g.ID, listGroupMembersOptions)
		if err != nil {
			return err
		}

		for _, m := range members {
			handleMember(g, m)
		}

		if resp.CurrentPage >= resp.TotalPages {
			break
		}

		listGroupMembersOptions.Page = resp.NextPage
	}

	return nil
}

// handleMember converts the Group and Project members into a single GenericMember type
// and will print it to the output unless its excluded by the members filters
func handleMember(container, member interface{}) {

	mem := &genericMember{}

	switch c := container.(type) {
	case *gitlab.Project:
		mem.Type = "project"
		mem.Path = c.PathWithNamespace
		mem.MembersSettingsURL = c.WebURL + "/-/project_members?search=" + member.(*gitlab.ProjectMember).Username
		mem.ContainerID = c.ID
	case *gitlab.Group:
		mem.Type = "group"
		mem.Path = c.FullPath
		mem.MembersSettingsURL = c.WebURL + "/-/group_members?search=" + member.(*gitlab.GroupMember).Username
		mem.ContainerID = c.ID
	}

	switch m := member.(type) {
	case *gitlab.ProjectMember:
		mem.Username = m.Username
		mem.AccessLevel = m.AccessLevel
		mem.UserId = m.ID
		mem.ExpiresAt = m.ExpiresAt
	case *gitlab.GroupMember:
		mem.Username = m.Username
		mem.AccessLevel = m.AccessLevel
		mem.UserId = m.ID
		mem.ExpiresAt = m.ExpiresAt
	}

	required := true

	for _, f := range memberFilters {
		if !f(mem) {
			required = false
			break
		}
	}

	if required {
		printMember(mem)
	}
}

func printMember(mem *genericMember) {
	var expires string

	if mem.ExpiresAt == nil {
		expires = ""
	} else {
		expires = mem.ExpiresAt.String()
	}

	fmt.Printf("%-10v %-50v %-30v %-20v %-15v %v\n", mem.Type, mem.Path, mem.Username, AccessLevelToString(mem.AccessLevel), expires, mem.MembersSettingsURL)
}

func AccessLevelToString(value gitlab.AccessLevelValue) string {

	switch value {
	case 0:
		return "none"
	case 10:
		return "guest"
	case 20:
		return "reporter"
	case 30:
		return "developer"
	case 40:
		return "maintainer"
	case 50:
		return "owner"
	default:
		return string(value)
	}
}

func Execute() {
	auditCmd := NewAuditCommand()
	if err := auditCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
