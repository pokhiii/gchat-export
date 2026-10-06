package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/OWNER/gchat-export/internal/model"
	"github.com/spf13/cobra"
)

var spaceTypes = map[string]model.SpaceType{
	"space": model.SpaceTypeSpace,
	"dm":    model.SpaceTypeDM,
	"group": model.SpaceTypeGroup,
}

func newSpacesCmd(a *app) *cobra.Command {
	var typ, filter string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "spaces",
		Short: "List the spaces, group chats and DMs you can export",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			want, ok := spaceTypes[typ]
			if typ != "" && !ok {
				return fmt.Errorf("--type must be space, dm or group")
			}
			c, _, err := a.chatClient(cmd.Context())
			if err != nil {
				return err
			}
			all, err := c.ListSpaces(cmd.Context())
			if err != nil {
				return err
			}
			var out []model.Space
			for _, s := range all {
				if typ != "" && s.Type != want {
					continue
				}
				if filter != "" && !strings.Contains(strings.ToLower(s.DisplayName), strings.ToLower(filter)) {
					continue
				}
				out = append(out, s)
			}
			if asJSON {
				enc := json.NewEncoder(a.stdout)
				enc.SetIndent("", "  ")
				if out == nil {
					out = []model.Space{}
				}
				return enc.Encode(out)
			}
			tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "SPACE\tTYPE\tNAME")
			for _, s := range out {
				name := termSafe(s.DisplayName)
				if name == "" {
					name = "(unnamed)"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, s.Type, name)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&typ, "type", "", "only show this kind: space, dm or group")
	cmd.Flags().StringVar(&filter, "filter", "", "only show spaces whose name contains this text")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}
