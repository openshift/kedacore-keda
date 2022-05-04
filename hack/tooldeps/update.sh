branch=$(git branch --show-current)
url="https://raw.githubusercontent.com/openshift/kedacore-keda/${branch}/Makefile"

cd -- "$( dirname -- "${BASH_SOURCE[0]}" )"

rm -f go.mod go.sum
go mod init tmp
for i in $(sed -n '/call go-get-tool/ s/^.*,\(.*\))/\1/p' ../../Makefile ); do
	go get -d $i
done
