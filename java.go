package main

type JavaData struct {
	jdk         string
	jarResource string
	srcResource string
	uberjar     bool
}

func (d *JavaData) apply(v UnknownStmt) bool {
	switch v.Name {
	case tokInvalid:
		if v.Raw.string() == "JAVA_SRCS" {
			throwFmt("JAVA_SRCS is not yet modelled")
		}

		return false
	case tokUberjar:
		d.uberjar = true
	case tokJdkVersion, tokJarResource, tokSrcResource:
		if len(v.Args) != 1 {
			throwFmt("%s requires one argument", v.Name)
		}

		value := v.Args[0].string()

		switch v.Name {
		case tokJdkVersion:
			d.jdk = value
		case tokJarResource:
			d.jarResource = value
		case tokSrcResource:
			d.srcResource = value
		}
	default:
		return false
	}

	return true
}
