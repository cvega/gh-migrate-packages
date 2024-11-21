// pkg/api/api.go
package api

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/gofri/go-github-ratelimit/github_ratelimit"
    "github.com/shurcooL/githubv4"
    "golang.org/x/oauth2"
    "github.com/cvega/gh-migrate-packages/pkg/package"
)

type RateLimitAwareGraphQLClient struct {
    client *githubv4.Client
}

func (c *RateLimitAwareGraphQLClient) Query(ctx context.Context, q interface{}, variables map[string]interface{}) error {
    var rateLimitQuery struct {
        RateLimit struct {
            Remaining int
            ResetAt   githubv4.DateTime
        }
    }

    for {
        if err := c.client.Query(ctx, &rateLimitQuery, nil); err != nil {
            return err
        }

        log.Printf("Rate limit remaining: %d\n", rateLimitQuery.RateLimit.Remaining)

        if rateLimitQuery.RateLimit.Remaining > 0 {
            return c.client.Query(ctx, q, variables)
        }

        log.Printf("Rate limit exceeded, sleeping until %v\n", rateLimitQuery.RateLimit.ResetAt.Time)
        time.Sleep(time.Until(rateLimitQuery.RateLimit.ResetAt.Time))
    }
}

type API struct {
    graphqlClient *RateLimitAwareGraphQLClient
    ctx           context.Context
}

func NewAPI(token, hostname string) *API {
    src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
    httpClient := oauth2.NewClient(context.Background(), src)
    
    rateLimiter, err := github_ratelimit.NewRateLimitWaiterClient(httpClient.Transport)
    if err != nil {
        log.Fatalf("Failed to create rate limiter: %v", err)
    }

    var baseClient *githubv4.Client
    if hostname != "" {
        baseClient = githubv4.NewEnterpriseClient(hostname+"/api/graphql", rateLimiter)
    } else {
        baseClient = githubv4.NewClient(rateLimiter)
    }

    return &API{
        graphqlClient: &RateLimitAwareGraphQLClient{client: baseClient},
        ctx:          context.Background(),
    }
}

// Query structures for GraphQL
type PackageQuery struct {
    Organization struct {
        Packages struct {
            PageInfo struct {
                EndCursor   githubv4.String
                HasNextPage bool
            }
            Nodes []struct {
                ID          githubv4.ID
                Name        githubv4.String
                PackageType githubv4.String
                Repository  struct {
                    Name githubv4.String
                    URL  githubv4.String
                }
                Statistics struct {
                    DownloadsTotalCount githubv4.Int
                }
                Versions struct {
                    Nodes []struct {
                        ID        githubv4.ID
                        Version   githubv4.String
                        Files struct {
                            Nodes []struct {
                                Name   githubv4.String
                                Size   githubv4.Int
                                SHA256 githubv4.String
                                URL    githubv4.URI
                            }
                        } `graphql:"files(first: 100)"`
                        Metadata struct {
                            PackageType githubv4.String
                        }
                        CreatedAt githubv4.DateTime
                        UpdatedAt githubv4.DateTime
                    }
                } `graphql:"versions(first: 100)"`
            }
        } `graphql:"packages(first: $first, after: $after, packageType: $packageType)"`
    } `graphql:"organization(login: $login)"`
}

func (a *API) GetOrganizationPackages(org, packageType string) ([]Package, error) {
    var query PackageQuery
    variables := map[string]interface{}{
        "login": githubv4.String(org),
        "first": githubv4.Int(100),
        "after": (*githubv4.String)(nil),
        "packageType": githubv4.String(packageType),
    }

    var packages []Package

    for {
        err := a.graphqlClient.Query(a.ctx, &query, variables)
        if err != nil {
            return nil, fmt.Errorf("failed to query packages: %v", err)
        }

        // Process packages from the current page
        for _, node := range query.Organization.Packages.Nodes {
            pkg := Package{
                ID:          string(node.ID.(string)),
                Name:        string(node.Name),
                PackageType: string(node.PackageType),
                Repository: &Repository{
                    Name: string(node.Repository.Name),
                    URL:  string(node.Repository.URL),
                },
                Statistics: &Statistics{
                    DownloadsCount: int(node.Statistics.DownloadsTotalCount),
                },
            }

            // Process versions
            for _, ver := range node.Versions.Nodes {
                version := Version{
                    ID:        string(ver.ID.(string)),
                    Name:      string(ver.Version),
                    CreatedAt: ver.CreatedAt.String(),
                    UpdatedAt: ver.UpdatedAt.String(),
                }

                // Process files
                for _, file := range ver.Files.Nodes {
                    version.Files = append(version.Files, File{
                        Name:   string(file.Name),
                        Size:   int(file.Size),
                        SHA256: string(file.SHA256),
                        URL:    string(file.URL),
                    })
                }

                pkg.Versions = append(pkg.Versions, version)
            }

            packages = append(packages, pkg)
        }

        // Check if there are more pages
        if !query.Organization.Packages.PageInfo.HasNextPage {
            break
        }

        // Update cursor for next page
        variables["after"] = githubv4.String(query.Organization.Packages.PageInfo.EndCursor)
    }

    return packages, nil
}

// Additional types moved from package/package.go
type Package struct {
    ID          string
    Name        string
    PackageType string
    Repository  *Repository
    Statistics  *Statistics
    Versions    []Version
}

type Version struct {
    ID        string
    Name      string
    Files     []File
    CreatedAt string
    UpdatedAt string
}

type File struct {
    Name   string
    Size   int
    SHA256 string
    URL    string
}

type Repository struct {
    Name string
    URL  string
}

type Statistics struct {
    DownloadsCount int
}
