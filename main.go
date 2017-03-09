package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/gosuri/uiprogress"
	"github.com/nicolasgomollon/peterplanner/helpers"
	"github.com/nicolasgomollon/peterplanner/parsers"
	"github.com/nicolasgomollon/peterplanner/types"
	terminal "github.com/wayneashleyberry/terminal-dimensions"
	"html"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const RootPath = "/var/www/"
const DegreeWorksURL = "https://www.reg.uci.edu/dgw/IRISLink.cgi"

func fetchStudentID(cookie string) (string, error) {
	body := "SERVICE=SCRIPTER&SCRIPT=SD2STUCON"
	statusCode, _, responseHTML, err := helpers.Post(DegreeWorksURL, cookie, body)
	if err != nil {
		return "", errors.New(fmt.Sprintf("ERROR: Unable to fetch DegreeWorks HTML file. `%v`.", err.Error()))
	} else if statusCode != http.StatusOK {
		return "", errors.New(fmt.Sprintf("ERROR: Unable to fetch DegreeWorks HTML file. HTTP status code: %v.", statusCode))
	}
	r, _ := regexp.Compile(`(?s)<input type="hidden" name="STUID" value="(\d*)">`)
	matches := r.FindStringSubmatch(responseHTML)
	if len(matches) == 0 {
		return "", errors.New(fmt.Sprintf("ERROR: Unable to fetch DegreeWorks HTML file. Invalid cookies."))
	}
	studentID := matches[1]
	return studentID, nil
}

func fetchStudentDetails(studentID, cookie string) (string, string, string, string, string, error) {
	body := fmt.Sprintf("SERVICE=SCRIPTER&SCRIPT=SD2STUGID&STUID=%s&DEBUG=OFF", studentID)
	statusCode, _, responseHTML, err := helpers.Post(DegreeWorksURL, cookie, body)
	if err != nil {
		return "", "", "", "", "", errors.New(fmt.Sprintf("ERROR: Unable to fetch DegreeWorks HTML file. `%v`.", err.Error()))
	} else if statusCode != http.StatusOK {
		return "", "", "", "", "", errors.New(fmt.Sprintf("ERROR: Unable to fetch DegreeWorks HTML file. HTTP status code: %v.", statusCode))
	}
	
	r, _ := regexp.Compile(`(?s)<StudentData>(.*)</StudentData>`)
	studentData := r.FindStringSubmatch(responseHTML)[1]
	
	r, _ = regexp.Compile(`(?s)<GoalDtl.*School="(?P<school>.*?)".*Degree="(?P<degree_code>.*?)".*StuLevel="(?P<student_level_code>.*?)".*</GoalDtl>.*<GoalDataDtl.*?GoalCode="MAJOR".*?GoalValue="(?P<major_code>.*?)".*?</GoalDataDtl>`)
	matches := r.FindStringSubmatch(studentData)
	groups := make(map[string]string)
	for i, name := range r.SubexpNames() {
		if i != 0 {
			groups[name] = matches[i]
		}
	}
	
	r, _ = regexp.Compile(fmt.Sprintf(`(?s)sMajorPicklist\[sMajorPicklist\.length\] = new DataItem\("%s *", "(.*?) *"\);`, groups["major_code"]))
	studentMajor := r.FindStringSubmatch(responseHTML)[1]
	
	r, _ = regexp.Compile(fmt.Sprintf(`(?s)sLevelPicklist\[sLevelPicklist\.length\] = new DataItem\("%s *", "(.*?) *"\);`, groups["student_level_code"]))
	studentLevel := r.FindStringSubmatch(responseHTML)[1]
	
	r, _ = regexp.Compile(fmt.Sprintf(`(?s)sDegreePicklist\[sDegreePicklist\.length\] = new DataItem\("%s *", "(.*?) *"\);`, groups["degree_code"]))
	degreeName := r.FindStringSubmatch(responseHTML)[1]
	
	return groups["school"], groups["degree_code"], degreeName, studentLevel, studentMajor, nil
}

func fetchXML(studentID, school, degree, degreeName, studentLevel, studentMajor, cookie string) (string, error) {
	studentLevel = html.UnescapeString(studentLevel)
	studentLevel = url.QueryEscape(studentLevel)
	studentMajor = html.UnescapeString(studentMajor)
	studentMajor = url.QueryEscape(studentMajor)
	body := fmt.Sprintf("SERVICE=SCRIPTER&REPORT=WEB31&SCRIPT=SD2GETAUD%%26ContentType%%3Dxml&USERID=%s&USERCLASS=STU&BROWSER=NOT-NAV4&ACTION=REVAUDIT&AUDITTYPE&DEGREETERM=ACTV&INTNOTES&INPROGRESS=N&CUTOFFTERM=ACTV&REFRESHBRDG=N&AUDITID&JSERRORCALL=SetError&NOTENUM&NOTETEXT&NOTEMODE&PENDING&INTERNAL&RELOADSEP=TRUE&PRELOADEDPLAN&ContentType=xml&STUID=%s&SCHOOL=%s&STUSCH=%s&DEGREE=%s&STUDEG=%s&STUDEGLIT=%s&STUDI&STULVL=%s&STUMAJLIT=%s&STUCATYEAR&CLASSES&DEBUG=OFF", studentID, studentID, school, school, degree, degree, degreeName, studentLevel, studentMajor)
	statusCode, _, responseXML, err := helpers.Post(DegreeWorksURL, cookie, body)
	if err != nil {
		return "", errors.New(fmt.Sprintf("ERROR: Unable to fetch DegreeWorks XML file. `%v`.", err.Error()))
	} else if statusCode != http.StatusOK {
		return "", errors.New(fmt.Sprintf("ERROR: Unable to fetch DegreeWorks XML file. HTTP status code: %v.", statusCode))
	}
	return responseXML, nil
}

type Department struct {
	Term   string
	Dept   string
	Option string
}

func fetchCourses(depts []string) {
	departments, err := parsers.AllDepartments()
	if err != nil {
		panic(err)
	}
	
	steps := make([]Department, 0)
	if len(depts) > 0 {
		for _, dept := range depts {
			if deptURL, ok := departments[dept]; ok {
				steps = append(steps, Department{Dept: dept, Option: deptURL})
			}
		}
	} else {
		for dept, deptURL := range departments {
			steps = append(steps, Department{Dept: dept, Option: deptURL})
		}
	}
	
	uiprogress.Start()
	width, _ := terminal.Width()
	bar := uiprogress.AddBar(len(steps) + 1)
	bar.Width = int(width) - (8 + 1) - (1 + 4)
	bar.AppendCompleted()
	
	bar.PrependFunc(func(b *uiprogress.Bar) string {
		step := "LOADING"
		if b.Current() == len(steps) {
			step = "DONE"
		} else if b.Current() > 0 {
			step = steps[b.Current()-1].Dept
		}
		return fmt.Sprintf("%-8s", step)
	})
	
	process := func(dept, deptURL string) {
		responseHTML, err := parsers.FetchCatalogue(deptURL)
		if err != nil {
			panic(err)
		}
		dir := strings.Replace(dept, "/", "_", -1)
		filepath := fmt.Sprintf(RootPath + "registrar/%v/", dir)
		os.MkdirAll(filepath, 0755)
		err = ioutil.WriteFile(filepath + "catalogue.html", []byte(responseHTML), 0644)
		if err != nil {
			panic(err)
		}
	}
	
	for _, d := range steps {
		bar.Incr()
		time.Sleep(10 * time.Second)
		process(d.Dept, d.Option)
	}
	bar.Incr()
}

func fetchPrereqs(depts []string) {
	term, deptOptions, err := parsers.PDepartmentOptions()
	if err != nil {
		panic(err)
	}
	
	steps := make([]Department, 0)
	if len(depts) > 0 {
		for _, dept := range depts {
			if option, ok := deptOptions[dept]; ok {
				steps = append(steps, Department{Dept: dept, Option: option})
			}
		}
	} else {
		for dept, option := range deptOptions {
			steps = append(steps, Department{Dept: dept, Option: option})
		}
	}
	
	uiprogress.Start()
	width, _ := terminal.Width()
	bar := uiprogress.AddBar(len(steps) + 1)
	bar.Width = int(width) - (8 + 1) - (1 + 4)
	bar.AppendCompleted()
	
	bar.PrependFunc(func(b *uiprogress.Bar) string {
		step := "LOADING"
		if b.Current() == len(steps) {
			step = "DONE"
		} else if b.Current() > 0 {
			step = steps[b.Current()-1].Dept
		}
		return fmt.Sprintf("%-8s", step)
	})
	
	process := func(dept, option string) {
		responseHTML, err := parsers.FetchPrerequisites(term, option)
		if err != nil {
			panic(err)
		}
		dir := strings.Replace(dept, "/", "_", -1)
		filepath := fmt.Sprintf(RootPath + "registrar/%v/", dir)
		os.MkdirAll(filepath, 0755)
		err = ioutil.WriteFile(filepath + "prereqs.html", []byte(responseHTML), 0644)
		if err != nil {
			panic(err)
		}
	}
	
	for _, d := range steps {
		bar.Incr()
		time.Sleep(10 * time.Second)
		process(d.Dept, d.Option)
	}
	bar.Incr()
}

func fetchSchedules(depts []string, archive bool) {
	term, deptOptions, err := parsers.SDepartmentOptions()
	if err != nil {
		panic(err)
	}
	if !types.IsAcademicTerm(term) {
		fmt.Println("WebSOC is not currently in an academic term.")
		return
	}
	
	steps := make([]Department, 0)
	if archive {
		yearTerm := term
		year := types.AcademicYear() + 1
		f := []interface{}{types.FallQuarter, types.SpringQuarter, types.WinterQuarter}
		
		for i := 1; i <= 9; i++ {
			if (i % 3) == 0 {
				year--
			}
			yearTerm = f[i % 3].(func(int) string)(year)
			if yearTerm > term {
				continue
			}
			
			if len(depts) > 0 {
				for _, dept := range depts {
					if option, ok := deptOptions[dept]; ok {
						steps = append(steps, Department{Term: yearTerm, Dept: dept, Option: option})
					}
				}
			} else {
				for dept, option := range deptOptions {
					steps = append(steps, Department{Term: yearTerm, Dept: dept, Option: option})
				}
			}
		}
	} else {
		if len(depts) > 0 {
			for _, dept := range depts {
				if option, ok := deptOptions[dept]; ok {
					steps = append(steps, Department{Term: term, Dept: dept, Option: option})
				}
			}
		} else {
			for dept, option := range deptOptions {
				steps = append(steps, Department{Term: term, Dept: dept, Option: option})
			}
		}
	}
	
	uiprogress.Start()
	width, _ := terminal.Width()
	bar := uiprogress.AddBar(len(steps) + 1)
	bar.Width = int(width) - (13 + 1) - (1 + 4)
	bar.AppendCompleted()
	
	bar.PrependFunc(func(b *uiprogress.Bar) string {
		step := "LOADING"
		if b.Current() == len(steps) {
			step = "DONE"
		} else if b.Current() > 0 {
			d := steps[b.Current()-1]
			t := ""
			if types.IsFQ(d.Term) {
				t = "F"
			} else if types.IsWQ(d.Term) {
				t = "W"
			} else if types.IsSQ(d.Term) {
				t = "S"
			}
			y := d.Term[2:4]
			t += y
			step = fmt.Sprintf("%v: %-8s", t, d.Dept)
		}
		return fmt.Sprintf("%-13s", step)
	})
	
	courseNums := make([]string, 0)
	process := func(yearTerm, dept, option string) {
		dir := strings.Replace(dept, "/", "_", -1)
		filepath := fmt.Sprintf(RootPath + "registrar/%v/", dir)
		socFile := filepath + fmt.Sprintf("soc_%v.txt", yearTerm)
		if archive {
			if _, err := os.Stat(socFile); err == nil {
				return
			}
		}
		time.Sleep(10 * time.Second)
		responseTXT, err := parsers.FetchWebSOC(yearTerm, option, courseNums)
		if err != nil {
			panic(err)
		}
		os.MkdirAll(filepath, 0755)
		err = ioutil.WriteFile(socFile, []byte(responseTXT), 0644)
		if err != nil {
			panic(err)
		}
	}
	
	for _, d := range steps {
		bar.Incr()
		process(d.Term, d.Dept, d.Option)
	}
	bar.Incr()
}

func main() {
	studentIDptr := flag.String("studentID", "", "Fetch DegreeWorks XML file for the specified student ID.")
	cookiePtr := flag.String("cookie", "", "Fetch DegreeWorks XML file using specified cookies.")
	cachePtr := flag.Bool("cache", false, "Cache fetched content on disk.")
	cataloguePtr := flag.Bool("catalogue", false, "Fetch courses from Course Catalogue.")
	prereqsPtr := flag.Bool("prereqs", false, "Fetch prerequisites from WebSOC.")
	schedulesPtr := flag.Bool("schedules", false, "Fetch schedules from WebSOC.")
	archivePtr := flag.Bool("archive", false, "Fetch the past two years of schedules from WebSOC.")
	dPtr := flag.String("d", "", "Comma-separated list of departments to use with `--prereqs` and `schedule` flags.")
	flag.Parse()
	
	depts := make([]string, 0)
	if len(*dPtr) > 0 {
		depts = strings.Split(*dPtr, ",")
	}
	
	if len(*cookiePtr) > 0 {
		if len(*studentIDptr) == 0 {
			studentID, err := fetchStudentID(*cookiePtr)
			if err != nil {
				panic(err)
			}
			*studentIDptr = studentID
		}
		
		school, degree, degreeName, studentLevel, studentMajor, err := fetchStudentDetails(*studentIDptr, *cookiePtr)
		if err != nil {
			panic(err)
		}
		
		responseXML, err := fetchXML(*studentIDptr, school, degree, degreeName, studentLevel, studentMajor, *cookiePtr)
		if err != nil {
			panic(err)
		}
		
		if *cachePtr {
			filepath := fmt.Sprintf(RootPath + "DGW_Report-%v.xsl", *studentIDptr)
			err = ioutil.WriteFile(filepath, []byte(responseXML), 0644)
			if err != nil {
				panic(err)
			}
		} else {
			fmt.Println(responseXML)
		}
	} else if *cataloguePtr {
		fetchCourses(depts)
	} else if *prereqsPtr {
		fetchPrereqs(depts)
	} else if *schedulesPtr {
		fetchSchedules(depts, *archivePtr)
	} else {
		fmt.Println("No flags were specified. Use `-h` or `--help` flags to get help.")
	}
}
