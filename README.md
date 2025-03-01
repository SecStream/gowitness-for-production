<h1 align="center">
  <br>
    🔍 gowitness
  <br>
  <br>
</h1>

<h4 align="center">A golang, web screenshot utility using Chrome Headless.</h4>

### This is clone repository. see author repo https://github.com/sensepost/gowitness


---

## Getting Started
```bash
make docker-image
docker compose up -d
Open https://gowitness.local
```

## Dev
```
gowiness report serve --address :7171 --fullpage --timeout 30 --delay 5 -t 1 --db-path postgresql://amnesia:amnesia@127.0.0.1:5432
```


## introduction

`gowitness` is a website screenshot utility written in Golang, that uses Chrome Headless to generate screenshots of web interfaces using the command line, with a handy report viewer to process results. Both Linux and macOS is supported, with Windows support mostly working.

Inspiration for `gowitness` comes from [Eyewitness](https://github.com/ChrisTruncer/EyeWitness). If you are looking for something with lots of extra features, be sure to check it out along with these [other](https://github.com/afxdub/http-screenshot-html) [projects](https://github.com/breenmachine/httpscreenshot).

## documentation

For installation information and other documentation, please refer to the wiki [here](https://github.com/sensepost/gowitness/wiki).

## screenshots

![dark](images/gowitness-detail.png)

## credits

`gowitness` would not have been posssible without these amazing projects:

- [chromedp](https://github.com/chromedp/chromedp)
- [tabler](https://github.com/tabler/tabler)
- [zerolog](https://github.com/rs/zerolog)
- [cobra](https://github.com/spf13/cobra)
- [gorm](https://github.com/go-gorm/gorm)
- [go-nmap](https://github.com/lair-framework/go-nmap)
- [wappalyzergo](https://github.com/projectdiscovery/wappalyzergo)
- [goimagehash](https://github.com/corona10/goimagehash)

And many more!

## license

`gowitness` is licensed under a [GNU General Public v3 License](https://www.gnu.org/licenses/gpl-3.0.en.html). Permissions beyond the scope of this license may be available at <http://sensepost.com/contact/>.
