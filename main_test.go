package main

import (
	"fmt"
	"github.com/xanzy/go-gitlab"
	"testing"
)

func TestCheckPaginationNeeded(t *testing.T) {

	tcs := []struct {
		itr   interface{}
		page  int
		total int
	}{
		{gitlab.ProjectMember{}, 1, 1},
		{[]*gitlab.Response{}, 1, 10},
	}

	for _, tc := range tcs {

		res := &gitlab.Response{
			CurrentPage: tc.page,
			TotalPages:  tc.total,
		}

		fmt.Println(tc.itr, res)
	}
}
