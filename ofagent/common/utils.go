/*
 * Copyright 2018-present Open Networking Foundation

 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at

 * http://www.apache.org/licenses/LICENSE-2.0

 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package ofagent

import (
        "github.com/opencord/voltha-go/common/log"
	"strings"
	"strconv"
	"go/types"
)

func pp() {
    log.Debugln("pp")
}

func mac_str_to_tuple(mac string) []int {
   log.Debugln("mac_str_to", mac)
   s := strings.Split(mac, ":")
   var arrI []int
   for i := 0; i <= len(s); i++ {
        i1, err := strconv.Atoi(s[i])
	if err == nil {
   	   arrI[i] = i1
	}
   } 
   return arrI
}

func mac_to_tuple(mac string) *types.Tuple {

   var theTuple *types.Tuple

   return theTuple
}
/*
func mac_str_to_tuple2(mac string) *Tuple {
   var tuple *Tuple
   tuple = NewTuple(mac_str_to_tuple(mac))
   return tuple
}
*/
/* 
def pp(obj):
    pp = PrettyPrinter(maxwidth=80)
    pp.pp(obj)
    return str(pp)

def mac_str_to_tuple(mac):
    """
    Convert 'xx:xx:xx:xx:xx:xx' MAC address string to a tuple of integers.
    Example: mac_str_to_tuple('00:01:02:03:04:05') == (0, 1, 2, 3, 4, 5)
    """
    return tuple(int(d, 16) for d in mac.split(':')) */
