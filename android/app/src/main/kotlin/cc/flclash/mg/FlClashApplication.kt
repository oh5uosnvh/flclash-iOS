package cc.flclash.mg

import android.app.Application
import android.content.Context
import cc.flclash.mg.common.GlobalState

class FlClashApplication : Application() {
    override fun attachBaseContext(base: Context?) {
        super.attachBaseContext(base)
        GlobalState.init(this)
    }
}
