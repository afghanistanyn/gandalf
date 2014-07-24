// Copyright 2014 gandalf authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package repository

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/tsuru/commandmocker"
	"github.com/tsuru/config"
	"github.com/tsuru/gandalf/db"
	"github.com/tsuru/gandalf/fs"
	fstesting "github.com/tsuru/tsuru/fs/testing"
	"io"
	"io/ioutil"
	"labix.org/v2/mgo/bson"
	"launchpad.net/gocheck"
	"path"
	"testing"
)

func Test(t *testing.T) { gocheck.TestingT(t) }

type S struct {
	tmpdir string
}

var _ = gocheck.Suite(&S{})

func (s *S) SetUpSuite(c *gocheck.C) {
	err := config.ReadConfigFile("../etc/gandalf.conf")
	c.Assert(err, gocheck.IsNil)
	config.Set("database:url", "127.0.0.1:27017")
	config.Set("database:name", "gandalf_repository_tests")
}

func (s *S) TearDownSuite(c *gocheck.C) {
	conn, err := db.Conn()
	c.Assert(err, gocheck.IsNil)
	defer conn.Close()
	conn.User().Database.DropDatabase()
}

func (s *S) TestNewShouldCreateANewRepository(c *gocheck.C) {
	tmpdir, err := commandmocker.Add("git", "$*")
	c.Assert(err, gocheck.IsNil)
	defer commandmocker.Remove(tmpdir)
	users := []string{"smeagol", "saruman"}
	r, err := New("myRepo", users, false)
	c.Assert(err, gocheck.IsNil)
	conn, err := db.Conn()
	c.Assert(err, gocheck.IsNil)
	defer conn.Close()
	defer conn.Repository().Remove(bson.M{"_id": "myRepo"})
	c.Assert(r.Name, gocheck.Equals, "myRepo")
	c.Assert(r.Users, gocheck.DeepEquals, users)
	c.Assert(r.IsPublic, gocheck.Equals, false)
}

func (s *S) TestNewShouldRecordItOnDatabase(c *gocheck.C) {
	tmpdir, err := commandmocker.Add("git", "$*")
	c.Assert(err, gocheck.IsNil)
	defer commandmocker.Remove(tmpdir)
	r, err := New("someRepo", []string{"smeagol"}, false)
	conn, err := db.Conn()
	c.Assert(err, gocheck.IsNil)
	defer conn.Close()
	defer conn.Repository().Remove(bson.M{"_id": "someRepo"})
	c.Assert(err, gocheck.IsNil)
	err = conn.Repository().Find(bson.M{"_id": "someRepo"}).One(&r)
	c.Assert(err, gocheck.IsNil)
	c.Assert(r.Name, gocheck.Equals, "someRepo")
	c.Assert(r.Users, gocheck.DeepEquals, []string{"smeagol"})
	c.Assert(r.IsPublic, gocheck.Equals, false)
}

func (s *S) TestNewPublicRepository(c *gocheck.C) {
	rfs := &fstesting.RecordingFs{FileContent: "foo"}
	fs.Fsystem = rfs
	defer func() { fs.Fsystem = nil }()
	tmpdir, err := commandmocker.Add("git", "$*")
	c.Assert(err, gocheck.IsNil)
	defer commandmocker.Remove(tmpdir)
	r, err := New("someRepo", []string{"smeagol"}, true)
	conn, err := db.Conn()
	c.Assert(err, gocheck.IsNil)
	defer conn.Close()
	defer conn.Repository().Remove(bson.M{"_id": "someRepo"})
	c.Assert(err, gocheck.IsNil)
	err = conn.Repository().Find(bson.M{"_id": "someRepo"}).One(&r)
	c.Assert(err, gocheck.IsNil)
	c.Assert(r.Name, gocheck.Equals, "someRepo")
	c.Assert(r.Users, gocheck.DeepEquals, []string{"smeagol"})
	c.Assert(r.IsPublic, gocheck.Equals, true)
	path := barePath("someRepo") + "/git-daemon-export-ok"
	c.Assert(rfs.HasAction("create "+path), gocheck.Equals, true)
}

func (s *S) TestNewBreaksOnValidationError(c *gocheck.C) {
	_, err := New("", []string{"smeagol"}, false)
	c.Check(err, gocheck.NotNil)
	expected := "Validation Error: repository name is not valid"
	got := err.Error()
	c.Assert(got, gocheck.Equals, expected)
}

func (s *S) TestRepositoryIsNotValidWithoutAName(c *gocheck.C) {
	r := Repository{Users: []string{"gollum"}, IsPublic: true}
	v, err := r.isValid()
	c.Assert(v, gocheck.Equals, false)
	c.Check(err, gocheck.NotNil)
	got := err.Error()
	expected := "Validation Error: repository name is not valid"
	c.Assert(got, gocheck.Equals, expected)
}

func (s *S) TestRepositoryIsNotValidWithInvalidName(c *gocheck.C) {
	r := Repository{Name: "foo bar", Users: []string{"gollum"}, IsPublic: true}
	v, err := r.isValid()
	c.Assert(v, gocheck.Equals, false)
	c.Check(err, gocheck.NotNil)
	got := err.Error()
	expected := "Validation Error: repository name is not valid"
	c.Assert(got, gocheck.Equals, expected)
}

func (s *S) TestRepositoryShoudBeInvalidWIthoutAnyUsers(c *gocheck.C) {
	r := Repository{Name: "foo_bar", Users: []string{}, IsPublic: true}
	v, err := r.isValid()
	c.Assert(v, gocheck.Equals, false)
	c.Assert(err, gocheck.NotNil)
	got := err.Error()
	expected := "Validation Error: repository should have at least one user"
	c.Assert(got, gocheck.Equals, expected)
}

func (s *S) TestRepositoryShouldBeValidWithoutIsPublic(c *gocheck.C) {
	r := Repository{Name: "someName", Users: []string{"smeagol"}}
	v, _ := r.isValid()
	c.Assert(v, gocheck.Equals, true)
}

func (s *S) TestNewShouldCreateNewGitBareRepository(c *gocheck.C) {
	tmpdir, err := commandmocker.Add("git", "$*")
	c.Assert(err, gocheck.IsNil)
	defer commandmocker.Remove(tmpdir)
	_, err = New("myRepo", []string{"pumpkin"}, true)
	c.Assert(err, gocheck.IsNil)
	conn, err := db.Conn()
	c.Assert(err, gocheck.IsNil)
	defer conn.Close()
	defer conn.Repository().Remove(bson.M{"_id": "myRepo"})
	c.Assert(commandmocker.Ran(tmpdir), gocheck.Equals, true)
}

func (s *S) TestNewShouldNotStoreRepoInDbWhenBareCreationFails(c *gocheck.C) {
	dir, err := commandmocker.Error("git", "", 1)
	c.Check(err, gocheck.IsNil)
	defer commandmocker.Remove(dir)
	r, err := New("myRepo", []string{"pumpkin"}, true)
	c.Check(err, gocheck.NotNil)
	conn, err := db.Conn()
	c.Assert(err, gocheck.IsNil)
	defer conn.Close()
	err = conn.Repository().Find(bson.M{"_id": r.Name}).One(&r)
	c.Assert(err, gocheck.ErrorMatches, "^not found$")
}

func (s *S) TestRemoveShouldRemoveBareRepositoryFromFileSystem(c *gocheck.C) {
	tmpdir, err := commandmocker.Add("git", "$*")
	c.Assert(err, gocheck.IsNil)
	defer commandmocker.Remove(tmpdir)
	rfs := &fstesting.RecordingFs{FileContent: "foo"}
	fs.Fsystem = rfs
	defer func() { fs.Fsystem = nil }()
	r, err := New("myRepo", []string{"pumpkin"}, false)
	c.Assert(err, gocheck.IsNil)
	err = Remove(r.Name)
	c.Assert(err, gocheck.IsNil)
	action := "removeall " + path.Join(bareLocation(), "myRepo.git")
	c.Assert(rfs.HasAction(action), gocheck.Equals, true)
}

func (s *S) TestRemoveShouldRemoveRepositoryFromDatabase(c *gocheck.C) {
	tmpdir, err := commandmocker.Add("git", "$*")
	c.Assert(err, gocheck.IsNil)
	defer commandmocker.Remove(tmpdir)
	rfs := &fstesting.RecordingFs{FileContent: "foo"}
	fs.Fsystem = rfs
	defer func() { fs.Fsystem = nil }()
	r, err := New("myRepo", []string{"pumpkin"}, false)
	c.Assert(err, gocheck.IsNil)
	err = Remove(r.Name)
	c.Assert(err, gocheck.IsNil)
	conn, err := db.Conn()
	c.Assert(err, gocheck.IsNil)
	defer conn.Close()
	err = conn.Repository().Find(bson.M{"_id": r.Name}).One(&r)
	c.Assert(err, gocheck.ErrorMatches, "^not found$")
}

func (s *S) TestRemoveShouldReturnMeaningfulErrorWhenRepositoryDoesNotExistsInDatabase(c *gocheck.C) {
	rfs := &fstesting.RecordingFs{FileContent: "foo"}
	fs.Fsystem = rfs
	defer func() { fs.Fsystem = nil }()
	r := &Repository{Name: "fooBar"}
	err := Remove(r.Name)
	c.Assert(err, gocheck.ErrorMatches, "^Could not remove repository: not found$")
}

func (s *S) TestRename(c *gocheck.C) {
	tmpdir, err := commandmocker.Add("git", "$*")
	c.Assert(err, gocheck.IsNil)
	repository, err := New("freedom", []string{"fss@corp.globo.com", "andrews@corp.globo.com"}, true)
	c.Check(err, gocheck.IsNil)
	commandmocker.Remove(tmpdir)
	rfs := &fstesting.RecordingFs{}
	fs.Fsystem = rfs
	defer func() { fs.Fsystem = nil }()
	err = Rename(repository.Name, "free")
	c.Assert(err, gocheck.IsNil)
	_, err = Get("freedom")
	c.Assert(err, gocheck.NotNil)
	repo, err := Get("free")
	c.Assert(err, gocheck.IsNil)
	repository.Name = "free"
	c.Assert(repo, gocheck.DeepEquals, *repository)
	action := "rename " + barePath("freedom") + " " + barePath("free")
	c.Assert(rfs.HasAction(action), gocheck.Equals, true)
}

func (s *S) TestRenameNotFound(c *gocheck.C) {
	err := Rename("something", "free")
	c.Assert(err, gocheck.NotNil)
}

func (s *S) TestReadOnlyURL(c *gocheck.C) {
	host, err := config.GetString("host")
	c.Assert(err, gocheck.IsNil)
	remote := (&Repository{Name: "lol"}).ReadOnlyURL()
	c.Assert(remote, gocheck.Equals, fmt.Sprintf("git://%s/lol.git", host))
}

func (s *S) TestReadOnlyURLWithSSH(c *gocheck.C) {
	config.Set("git:ssh:use", true)
	defer config.Unset("git:ssh:use")
	host, err := config.GetString("host")
	c.Assert(err, gocheck.IsNil)
	remote := (&Repository{Name: "lol"}).ReadOnlyURL()
	c.Assert(remote, gocheck.Equals, fmt.Sprintf("ssh://git@%s/lol.git", host))
}

func (s *S) TestReadOnlyURLWithSSHAndPort(c *gocheck.C) {
	config.Set("git:ssh:use", true)
	defer config.Unset("git:ssh:use")
	config.Set("git:ssh:port", "49022")
	defer config.Unset("git:ssh:port")
	host, err := config.GetString("host")
	c.Assert(err, gocheck.IsNil)
	remote := (&Repository{Name: "lol"}).ReadOnlyURL()
	c.Assert(remote, gocheck.Equals, fmt.Sprintf("ssh://git@%s:49022/lol.git", host))
}

func (s *S) TestReadOnlyURLWithReadOnlyHost(c *gocheck.C) {
	config.Set("readonly-host", "something-private")
	defer config.Unset("readonly-host")
	remote := (&Repository{Name: "lol"}).ReadOnlyURL()
	c.Assert(remote, gocheck.Equals, "git://something-private/lol.git")
}

func (s *S) TestReadWriteURLWithSSH(c *gocheck.C) {
	config.Set("git:ssh:use", true)
	defer config.Unset("git:ssh:use")
	uid, err := config.GetString("uid")
	c.Assert(err, gocheck.IsNil)
	host, err := config.GetString("host")
	c.Assert(err, gocheck.IsNil)
	remote := (&Repository{Name: "lol"}).ReadWriteURL()
	expected := fmt.Sprintf("ssh://%s@%s/lol.git", uid, host)
	c.Assert(remote, gocheck.Equals, expected)
}

func (s *S) TestReadWriteURLWithSSHAndPort(c *gocheck.C) {
	config.Set("git:ssh:use", true)
	defer config.Unset("git:ssh:use")
	config.Set("git:ssh:port", "49022")
	defer config.Unset("git:ssh:port")
	uid, err := config.GetString("uid")
	c.Assert(err, gocheck.IsNil)
	host, err := config.GetString("host")
	c.Assert(err, gocheck.IsNil)
	remote := (&Repository{Name: "lol"}).ReadWriteURL()
	expected := fmt.Sprintf("ssh://%s@%s:49022/lol.git", uid, host)
	c.Assert(remote, gocheck.Equals, expected)
}

func (s *S) TestReadWriteURL(c *gocheck.C) {
	host, err := config.GetString("host")
	c.Assert(err, gocheck.IsNil)
	remote := (&Repository{Name: "lol"}).ReadWriteURL()
	c.Assert(remote, gocheck.Equals, fmt.Sprintf("git@%s:lol.git", host))
}

func (s *S) TestReadWriteURLUseUidFromConfigFile(c *gocheck.C) {
	uid, err := config.GetString("uid")
	c.Assert(err, gocheck.IsNil)
	host, err := config.GetString("host")
	c.Assert(err, gocheck.IsNil)
	config.Set("uid", "test")
	defer config.Set("uid", uid)
	remote := (&Repository{Name: "f#"}).ReadWriteURL()
	c.Assert(remote, gocheck.Equals, fmt.Sprintf("test@%s:f#.git", host))
}

func (s *S) TestGrantAccessShouldAddUserToListOfRepositories(c *gocheck.C) {
	tmpdir, err := commandmocker.Add("git", "$*")
	c.Assert(err, gocheck.IsNil)
	defer commandmocker.Remove(tmpdir)
	r, err := New("proj1", []string{"someuser"}, true)
	c.Assert(err, gocheck.IsNil)
	conn, err := db.Conn()
	c.Assert(err, gocheck.IsNil)
	defer conn.Close()
	defer conn.Repository().RemoveId(r.Name)
	r2, err := New("proj2", []string{"otheruser"}, true)
	c.Assert(err, gocheck.IsNil)
	defer conn.Repository().RemoveId(r2.Name)
	u := struct {
		Name string `bson:"_id"`
	}{Name: "lolcat"}
	err = conn.User().Insert(&u)
	c.Assert(err, gocheck.IsNil)
	defer conn.User().RemoveId(u.Name)
	err = GrantAccess([]string{r.Name, r2.Name}, []string{u.Name})
	c.Assert(err, gocheck.IsNil)
	err = conn.Repository().FindId(r.Name).One(&r)
	c.Assert(err, gocheck.IsNil)
	err = conn.Repository().FindId(r2.Name).One(&r2)
	c.Assert(err, gocheck.IsNil)
	c.Assert(r.Users, gocheck.DeepEquals, []string{"someuser", u.Name})
	c.Assert(r2.Users, gocheck.DeepEquals, []string{"otheruser", u.Name})
}

func (s *S) TestGrantAccessShouldAddFirstUserIntoRepositoryDocument(c *gocheck.C) {
	r := Repository{Name: "proj1"}
	conn, err := db.Conn()
	c.Assert(err, gocheck.IsNil)
	defer conn.Close()
	err = conn.Repository().Insert(&r)
	c.Assert(err, gocheck.IsNil)
	defer conn.Repository().RemoveId(r.Name)
	r2 := Repository{Name: "proj2"}
	err = conn.Repository().Insert(&r2)
	c.Assert(err, gocheck.IsNil)
	defer conn.Repository().RemoveId(r2.Name)
	err = GrantAccess([]string{r.Name, r2.Name}, []string{"Umi"})
	c.Assert(err, gocheck.IsNil)
	err = conn.Repository().FindId(r.Name).One(&r)
	c.Assert(err, gocheck.IsNil)
	err = conn.Repository().FindId(r.Name).One(&r2)
	c.Assert(err, gocheck.IsNil)
	c.Assert(r.Users, gocheck.DeepEquals, []string{"Umi"})
	c.Assert(r2.Users, gocheck.DeepEquals, []string{"Umi"})
}

func (s *S) TestGrantAccessShouldSkipDuplicatedUsers(c *gocheck.C) {
	r := Repository{Name: "proj1", Users: []string{"umi", "luke", "pade"}}
	conn, err := db.Conn()
	c.Assert(err, gocheck.IsNil)
	defer conn.Close()
	err = conn.Repository().Insert(&r)
	c.Assert(err, gocheck.IsNil)
	defer conn.Repository().RemoveId(r.Name)
	err = GrantAccess([]string{r.Name}, []string{"pade"})
	c.Assert(err, gocheck.IsNil)
	err = conn.Repository().FindId(r.Name).One(&r)
	c.Assert(err, gocheck.IsNil)
	c.Assert(r.Users, gocheck.DeepEquals, []string{"umi", "luke", "pade"})
}

func (s *S) TestRevokeAccessShouldRemoveUserFromAllRepositories(c *gocheck.C) {
	tmpdir, err := commandmocker.Add("git", "$*")
	c.Assert(err, gocheck.IsNil)
	defer commandmocker.Remove(tmpdir)
	r, err := New("proj1", []string{"someuser", "umi"}, true)
	c.Assert(err, gocheck.IsNil)
	conn, err := db.Conn()
	c.Assert(err, gocheck.IsNil)
	defer conn.Close()
	defer conn.Repository().RemoveId(r.Name)
	r2, err := New("proj2", []string{"otheruser", "umi"}, true)
	c.Assert(err, gocheck.IsNil)
	defer conn.Repository().RemoveId(r2.Name)
	err = RevokeAccess([]string{r.Name, r2.Name}, []string{"umi"})
	c.Assert(err, gocheck.IsNil)
	err = conn.Repository().FindId(r.Name).One(&r)
	c.Assert(err, gocheck.IsNil)
	err = conn.Repository().FindId(r2.Name).One(&r2)
	c.Assert(err, gocheck.IsNil)
	c.Assert(r.Users, gocheck.DeepEquals, []string{"someuser"})
	c.Assert(r2.Users, gocheck.DeepEquals, []string{"otheruser"})
}

func (s *S) TestConflictingRepositoryNameShouldReturnExplicitError(c *gocheck.C) {
	tmpdir, err := commandmocker.Add("git", "$*")
	c.Assert(err, gocheck.IsNil)
	defer commandmocker.Remove(tmpdir)
	_, err = New("someRepo", []string{"gollum"}, true)
	conn, err := db.Conn()
	c.Assert(err, gocheck.IsNil)
	defer conn.Close()
	defer conn.Repository().Remove(bson.M{"_id": "someRepo"})
	c.Assert(err, gocheck.IsNil)
	_, err = New("someRepo", []string{"gollum"}, true)
	c.Assert(err, gocheck.ErrorMatches, "A repository with this name already exists.")
}

func (s *S) TestGet(c *gocheck.C) {
	repo := Repository{Name: "somerepo", Users: []string{}}
	conn, err := db.Conn()
	c.Assert(err, gocheck.IsNil)
	defer conn.Close()
	err = conn.Repository().Insert(repo)
	c.Assert(err, gocheck.IsNil)
	r, err := Get("somerepo")
	c.Assert(err, gocheck.IsNil)
	c.Assert(r, gocheck.DeepEquals, repo)
}

func (s *S) TestMarshalJSON(c *gocheck.C) {
	repo := Repository{Name: "somerepo", Users: []string{}}
	expected := map[string]interface{}{
		"name":    repo.Name,
		"public":  repo.IsPublic,
		"ssh_url": repo.ReadWriteURL(),
		"git_url": repo.ReadOnlyURL(),
	}
	data, err := json.Marshal(&repo)
	c.Assert(err, gocheck.IsNil)
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	c.Assert(err, gocheck.IsNil)
	c.Assert(result, gocheck.DeepEquals, expected)
}

func (s *S) TestGetFileContentsWhenContentsAvailable(c *gocheck.C) {
	expected := []byte("something")
	Retriever = &MockContentRetriever{
		ResultContents: expected,
	}
	defer func() {
		Retriever = nil
	}()
	contents, err := GetFileContents("repo", "ref", "path")
	c.Assert(err, gocheck.IsNil)
	c.Assert(string(contents), gocheck.Equals, string(expected))
}

func (s *S) TestGetFileContentsWhenGitNotFound(c *gocheck.C) {
	lookpathError := fmt.Errorf("mock lookpath error")
	Retriever = &MockContentRetriever{
		LookPathError: lookpathError,
	}
	defer func() {
		Retriever = nil
	}()
	_, err := GetFileContents("repo", "ref", "path")
	c.Assert(err.Error(), gocheck.Equals, "mock lookpath error")
}

func (s *S) TestGetFileContentsWhenCommandFails(c *gocheck.C) {
	outputError := fmt.Errorf("mock output error")
	Retriever = &MockContentRetriever{
		OutputError: outputError,
	}
	defer func() {
		Retriever = nil
	}()
	_, err := GetFileContents("repo", "ref", "path")
	c.Assert(err.Error(), gocheck.Equals, "mock output error")
}

func (s *S) TestGetArchive(c *gocheck.C) {
	expected := []byte("something")
	Retriever = &MockContentRetriever{
		ResultContents: expected,
	}
	defer func() {
		Retriever = nil
	}()
	contents, err := GetArchive("repo", "ref", Zip)
	c.Assert(err, gocheck.IsNil)
	c.Assert(string(contents), gocheck.Equals, string(expected))
}

func (s *S) TestGetArchiveWhenGitNotFound(c *gocheck.C) {
	lookpathError := fmt.Errorf("mock lookpath error")
	Retriever = &MockContentRetriever{
		LookPathError: lookpathError,
	}
	defer func() {
		Retriever = nil
	}()
	_, err := GetArchive("repo", "ref", Zip)
	c.Assert(err.Error(), gocheck.Equals, "mock lookpath error")
}

func (s *S) TestGetArchiveWhenCommandFails(c *gocheck.C) {
	outputError := fmt.Errorf("mock output error")
	Retriever = &MockContentRetriever{
		OutputError: outputError,
	}
	defer func() {
		Retriever = nil
	}()
	_, err := GetArchive("repo", "ref", Zip)
	c.Assert(err.Error(), gocheck.Equals, "mock output error")
}

func (s *S) TestGetFileContentIntegration(c *gocheck.C) {
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "README"
	content := "much WOW"
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content)
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	contents, err := GetFileContents(repo, "master", file)
	c.Assert(err, gocheck.IsNil)
	c.Assert(string(contents), gocheck.Equals, content)
}

func (s *S) TestGetFileContentIntegrationEmptyContent(c *gocheck.C) {
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "README"
	content := ""
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content)
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	contents, err := GetFileContents(repo, "master", file)
	c.Assert(err, gocheck.IsNil)
	c.Assert(string(contents), gocheck.Equals, content)
}

func (s *S) TestGetFileContentWhenRefIsInvalid(c *gocheck.C) {
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "README"
	content := "much WOW"
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content)
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	_, err := GetFileContents(repo, "MuchMissing", file)
	c.Assert(err, gocheck.ErrorMatches, "^Error when trying to obtain file README on ref MuchMissing of repository gandalf-test-repo \\(exit status 128\\)\\.$")
}

func (s *S) TestGetFileContentWhenFileIsInvalid(c *gocheck.C) {
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "README"
	content := "much WOW"
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content)
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	_, err := GetFileContents(repo, "master", "Such file")
	c.Assert(err, gocheck.ErrorMatches, "^Error when trying to obtain file Such file on ref master of repository gandalf-test-repo \\(exit status 128\\)\\.$")
}

func (s *S) TestGetTreeIntegration(c *gocheck.C) {
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "README"
	content := "much WOW"
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content, "such", "folder", "much", "magic")
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	tree, err := GetTree(repo, "master", "much/README")
	c.Assert(err, gocheck.IsNil)
	c.Assert(tree[0]["path"], gocheck.Equals, "much/README")
	c.Assert(tree[0]["rawPath"], gocheck.Equals, "much/README")
}

func (s *S) TestGetTreeIntegrationEmptyContent(c *gocheck.C) {
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "README"
	content := ""
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content, "such", "folder", "much", "magic")
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	tree, err := GetTree(repo, "master", "much/README")
	c.Assert(err, gocheck.IsNil)
	c.Assert(tree[0]["path"], gocheck.Equals, "much/README")
	c.Assert(tree[0]["rawPath"], gocheck.Equals, "much/README")
}

func (s *S) TestGetTreeIntegrationWithEscapedFileName(c *gocheck.C) {
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "such\tREADME"
	content := "much WOW"
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content, "such", "folder", "much", "magic")
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	tree, err := GetTree(repo, "master", "much/such\tREADME")
	c.Assert(err, gocheck.IsNil)
	c.Assert(tree[0]["path"], gocheck.Equals, "much/such\\tREADME")
	c.Assert(tree[0]["rawPath"], gocheck.Equals, "\"much/such\\tREADME\"")
}

func (s *S) TestGetTreeIntegrationWithFileNameWithSpace(c *gocheck.C) {
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "much README"
	content := "much WOW"
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content, "such", "folder", "much", "magic")
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	tree, err := GetTree(repo, "master", "much/much README")
	c.Assert(err, gocheck.IsNil)
	c.Assert(tree[0]["path"], gocheck.Equals, "much/much README")
	c.Assert(tree[0]["rawPath"], gocheck.Equals, "much/much README")
}

func (s *S) TestGetArchiveIntegrationWhenZip(c *gocheck.C) {
	expected := make(map[string]string)
	expected["gandalf-test-repo-master/README"] = "much WOW"
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "README"
	content := "much WOW"
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content)
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	zipContents, err := GetArchive(repo, "master", Zip)
	reader := bytes.NewReader(zipContents)
	zipReader, err := zip.NewReader(reader, int64(len(zipContents)))
	c.Assert(err, gocheck.IsNil)
	for _, f := range zipReader.File {
		//fmt.Printf("Contents of %s:\n", f.Name)
		rc, err := f.Open()
		c.Assert(err, gocheck.IsNil)
		defer rc.Close()
		contents, err := ioutil.ReadAll(rc)
		c.Assert(err, gocheck.IsNil)
		c.Assert(string(contents), gocheck.Equals, expected[f.Name])
	}
}

func (s *S) TestGetArchiveIntegrationWhenTar(c *gocheck.C) {
	expected := make(map[string]string)
	expected["gandalf-test-repo-master/README"] = "much WOW"
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "README"
	content := "much WOW"
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content)
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	tarContents, err := GetArchive(repo, "master", Tar)
	c.Assert(err, gocheck.IsNil)
	reader := bytes.NewReader(tarContents)
	tarReader := tar.NewReader(reader)
	c.Assert(err, gocheck.IsNil)
	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			// end of tar archive
			break
		}
		c.Assert(err, gocheck.IsNil)
		path := hdr.Name
		_, ok := expected[path]
		if !ok {
			continue
		}
		buffer := new(bytes.Buffer)
		_, err = io.Copy(buffer, tarReader)
		c.Assert(err, gocheck.IsNil)
		c.Assert(buffer.String(), gocheck.Equals, expected[path])
	}
}

func (s *S) TestGetArchiveIntegrationWhenInvalidFormat(c *gocheck.C) {
	expected := make(map[string]string)
	expected["gandalf-test-repo-master/README"] = "much WOW"
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "README"
	content := "much WOW"
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content)
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	zipContents, err := GetArchive(repo, "master", 99)
	reader := bytes.NewReader(zipContents)
	zipReader, err := zip.NewReader(reader, int64(len(zipContents)))
	c.Assert(err, gocheck.IsNil)
	for _, f := range zipReader.File {
		//fmt.Printf("Contents of %s:\n", f.Name)
		rc, err := f.Open()
		c.Assert(err, gocheck.IsNil)
		defer rc.Close()
		contents, err := ioutil.ReadAll(rc)
		c.Assert(err, gocheck.IsNil)
		c.Assert(string(contents), gocheck.Equals, expected[f.Name])
	}
}

func (s *S) TestGetArchiveIntegrationWhenInvalidRepo(c *gocheck.C) {
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "README"
	content := "much WOW"
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content)
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	_, err := GetArchive("invalid-repo", "master", Zip)
	c.Assert(err.Error(), gocheck.Equals, "Error when trying to obtain archive for ref master of repository invalid-repo (Repository does not exist).")
}

func (s *S) TestGetTreeIntegrationWithMissingFile(c *gocheck.C) {
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "README"
	content := "very WOW"
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content)
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	tree, err := GetTree(repo, "master", "very missing")
	c.Assert(err, gocheck.IsNil)
	c.Assert(tree, gocheck.HasLen, 0)
}

func (s *S) TestGetTreeIntegrationWithInvalidRef(c *gocheck.C) {
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "README"
	content := "very WOW"
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content)
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	_, err := GetTree(repo, "VeryInvalid", "very missing")
	c.Assert(err, gocheck.ErrorMatches, "^Error when trying to obtain tree very missing on ref VeryInvalid of repository gandalf-test-repo \\(exit status 128\\)\\.$")
}

func (s *S) TestGetBranchIntegration(c *gocheck.C) {
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "README"
	content := "will bark"
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content)
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	errCreateBranches := CreateBranchesOnTestRepository(bare, repo, "doge_bites", "doge_barks")
	c.Assert(errCreateBranches, gocheck.IsNil)
	branches, err := GetBranch(repo)
	c.Assert(err, gocheck.IsNil)
	c.Assert(len(branches), gocheck.Equals, 3)
	c.Assert(branches[0]["ref"], gocheck.Matches, "[a-f0-9]{40}")
	c.Assert(branches[0]["name"], gocheck.Equals, "doge_barks")
	c.Assert(branches[0]["commiterName"], gocheck.Equals, "doge")
	c.Assert(branches[0]["commiterEmail"], gocheck.Equals, "<much@email.com>")
	c.Assert(branches[0]["authorName"], gocheck.Equals, "doge")
	c.Assert(branches[0]["authorEmail"], gocheck.Equals, "<much@email.com>")
	c.Assert(branches[0]["subject"], gocheck.Equals, "will bark")
	c.Assert(branches[1]["ref"], gocheck.Matches, "[a-f0-9]{40}")
	c.Assert(branches[1]["name"], gocheck.Equals, "doge_bites")
	c.Assert(branches[1]["commiterName"], gocheck.Equals, "doge")
	c.Assert(branches[1]["commiterEmail"], gocheck.Equals, "<much@email.com>")
	c.Assert(branches[1]["authorName"], gocheck.Equals, "doge")
	c.Assert(branches[1]["authorEmail"], gocheck.Equals, "<much@email.com>")
	c.Assert(branches[1]["subject"], gocheck.Equals, "will bark")
	c.Assert(branches[2]["ref"], gocheck.Matches, "[a-f0-9]{40}")
	c.Assert(branches[2]["name"], gocheck.Equals, "master")
	c.Assert(branches[2]["commiterName"], gocheck.Equals, "doge")
	c.Assert(branches[2]["commiterEmail"], gocheck.Equals, "<much@email.com>")
	c.Assert(branches[2]["authorName"], gocheck.Equals, "doge")
	c.Assert(branches[2]["authorEmail"], gocheck.Equals, "<much@email.com>")
	c.Assert(branches[2]["subject"], gocheck.Equals, "will bark")
}

func (s *S) TestGetForEachRefIntegrationEmptySubject(c *gocheck.C) {
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "README"
	content := ""
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content)
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	errCreateBranches := CreateBranchesOnTestRepository(bare, repo, "doge_howls")
	c.Assert(errCreateBranches, gocheck.IsNil)
	refs, err := GetForEachRef(repo, "refs/")
	c.Assert(err, gocheck.IsNil)
	c.Assert(len(refs), gocheck.Equals, 2)
	c.Assert(refs[0]["ref"], gocheck.Matches, "[a-f0-9]{40}")
	c.Assert(refs[0]["name"], gocheck.Equals, "doge_howls")
	c.Assert(refs[0]["commiterName"], gocheck.Equals, "doge")
	c.Assert(refs[0]["commiterEmail"], gocheck.Equals, "<much@email.com>")
	c.Assert(refs[0]["authorName"], gocheck.Equals, "doge")
	c.Assert(refs[0]["authorEmail"], gocheck.Equals, "<much@email.com>")
	c.Assert(refs[0]["subject"], gocheck.Equals, "")
	c.Assert(refs[1]["ref"], gocheck.Matches, "[a-f0-9]{40}")
	c.Assert(refs[1]["name"], gocheck.Equals, "master")
	c.Assert(refs[1]["commiterName"], gocheck.Equals, "doge")
	c.Assert(refs[1]["commiterEmail"], gocheck.Equals, "<much@email.com>")
	c.Assert(refs[1]["authorName"], gocheck.Equals, "doge")
	c.Assert(refs[1]["authorEmail"], gocheck.Equals, "<much@email.com>")
	c.Assert(refs[1]["subject"], gocheck.Equals, "")
}

func (s *S) TestGetForEachRefIntegrationSubjectWithTab(c *gocheck.C) {
	oldBare := bare
	bare = "/tmp"
	repo := "gandalf-test-repo"
	file := "README"
	content := "will\tbark"
	cleanUp, errCreate := CreateTestRepository(bare, repo, file, content)
	defer func() {
		cleanUp()
		bare = oldBare
	}()
	c.Assert(errCreate, gocheck.IsNil)
	errCreateBranches := CreateBranchesOnTestRepository(bare, repo, "doge_howls")
	c.Assert(errCreateBranches, gocheck.IsNil)
	refs, err := GetForEachRef(repo, "refs/")
	c.Assert(err, gocheck.IsNil)
	c.Assert(len(refs), gocheck.Equals, 2)
	c.Assert(refs[0]["ref"], gocheck.Matches, "[a-f0-9]{40}")
	c.Assert(refs[0]["name"], gocheck.Equals, "doge_howls")
	c.Assert(refs[0]["commiterName"], gocheck.Equals, "doge")
	c.Assert(refs[0]["commiterEmail"], gocheck.Equals, "<much@email.com>")
	c.Assert(refs[0]["authorName"], gocheck.Equals, "doge")
	c.Assert(refs[0]["authorEmail"], gocheck.Equals, "<much@email.com>")
	c.Assert(refs[0]["subject"], gocheck.Equals, "will\tbark")
	c.Assert(refs[1]["ref"], gocheck.Matches, "[a-f0-9]{40}")
	c.Assert(refs[1]["name"], gocheck.Equals, "master")
	c.Assert(refs[1]["commiterName"], gocheck.Equals, "doge")
	c.Assert(refs[1]["commiterEmail"], gocheck.Equals, "<much@email.com>")
	c.Assert(refs[1]["authorName"], gocheck.Equals, "doge")
	c.Assert(refs[1]["authorEmail"], gocheck.Equals, "<much@email.com>")
	c.Assert(refs[1]["subject"], gocheck.Equals, "will\tbark")
}
